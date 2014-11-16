package main

import (
	"github.com/seehuhn/trace"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Proxy struct {
	Name     string
	upstream http.RoundTripper
	cache    Cache
	logger   chan<- *LogEntry
	AdminMux *http.ServeMux
	shared   bool
}

func NewProxy(name string, transport http.RoundTripper, cache Cache, shared bool) *Proxy {
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &Proxy{
		Name:     name,
		upstream: transport,
		cache:    cache,
		logger:   NewLogger(),
		AdminMux: http.NewServeMux(),
		shared:   shared,
	}
}

func (proxy *Proxy) Close() error {
	return proxy.cache.Close()
}

func (proxy *Proxy) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.URL.Host == "" || req.URL.Host == proxy.Name {
		proxy.AdminMux.ServeHTTP(w, req)
		return
	}

	requestTime := time.Now()
	log := &LogEntry{
		RequestTimeNano: requestTime.UnixNano(),
		RemoteAddr:      req.RemoteAddr,
		Method:          req.Method,
		RequestURI:      req.RequestURI,
	}
	defer func() {
		log.HandlerCompleteNano =
			int64(time.Since(requestTime) / time.Nanosecond)
		proxy.logger <- log
	}()

	cacheInfo := proxy.getCacheability(req)

	var respData *proxyResponse

	// step 1: check whether any cached responses are available
	choices := []*proxyResponse{}
	if cacheInfo.canServeFromCache {
		choices = proxy.cache.Retrieve(req)
		if len(choices) > 0 {
			sort.Sort(byDate(choices))
			respData = choices[0]
		}
	}

	// step 2: if the responses are stale, send a validation request
	stale := true
	if stale {
		respData = proxy.requestFromUpstream(req, choices)
	}

	// step 3: make sure we still have the body of the selected response
	if respData != nil && respData.body == nil {
		respData.body = respData.getBody()
		if respData.body == nil {
			respData = nil
		}
	}

	// step 4: if the above fails, forward the request upstream
	if respData == nil {
		respData = proxy.requestFromUpstream(req, nil)
	}

	log.ResponseReceivedNano = int64(time.Since(requestTime) / time.Nanosecond)
	log.StatusCode = respData.StatusCode

	proxy.updateCacheability(respData, cacheInfo)
	log.Comments = append(log.Comments, cacheInfo.log...)

	h := w.Header()
	copyHeader(h, respData.Header)
	w.WriteHeader(respData.StatusCode)

	if respData.body == nil {
		respData.body = respData.getBody()
	}
	defer respData.body.Close()

	var n int64
	var err error
	if cacheInfo.canStore {
		entry := proxy.cache.StoreStart(
			req.URL.String(), respData.StatusCode, respData.Header)
		n, err = io.Copy(w, entry.Reader(respData.body))
		if err != nil {
			entry.Abort()
		} else {
			entry.Complete()
		}
		log.CacheResult = "MISS,STORE"
	} else {
		n, err = io.Copy(w, respData.body)
		if log.CacheResult == "" {
			log.CacheResult = "MISS,NOSTORE"
		}
	}
	if err != nil {
		trace.T("jvproxy/handler", trace.PrioDebug,
			"error while writing response: %s", err.Error())
	}
	log.ContentLength = n
	// TODO(voss): compare n to the server-provided Content-Length
}

type proxyResponse struct {
	StatusCode int
	Header     http.Header
	source     string
	body       io.ReadCloser
	getBody    func() io.ReadCloser
}

type byDate []*proxyResponse

func (x byDate) Len() int      { return len(x) }
func (x byDate) Swap(i, j int) { x[i], x[j] = x[j], x[i] }
func (x byDate) Less(i, j int) bool {
	dateI := parseDate(x[i].Header.Get("Date"))
	dateJ := parseDate(x[i].Header.Get("Date"))
	return dateI.After(dateJ)
}

// Hop-by-hop headers, as specified in
// http://www.w3.org/Protocols/rfc2616/rfc2616-sec13.html
var perHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te", // canonicalized version of "TE"
	"Trailers",
	"Transfer-Encoding",
	"Upgrade",
}

// requestFromUpstream forwards a client request to the upstream
// server.  If `stale` is set, it should be a slice of cached
// responses; in this case a validation request is sent which asks the
// server to select one of the available responses.
//
// `stale` must be ordered in order of the Date header field, the
// newest item first.
func (proxy *Proxy) requestFromUpstream(req *http.Request, stale []*proxyResponse) *proxyResponse {
	upReq := new(http.Request)
	*upReq = *req // includes shallow copies of maps, care is needed below ...
	upReq.Proto = "HTTP/1.1"
	upReq.ProtoMajor = 1
	upReq.ProtoMinor = 1
	upReq.Close = false

	// Remove hop-by-hop headers to upstream.  Especially important is
	// "Connection" because we want a persistent connection,
	// regardless of what the client sent to us.  upReq is sharing the
	// underlying map from req (shallow copied above), we copy it if
	// necessary.
	copiedHeaders := false
	for _, name := range perHopHeaders {
		if upReq.Header.Get(name) != "" {
			if !copiedHeaders {
				upReq.Header = make(http.Header)
				copyHeader(upReq.Header, req.Header)
				copiedHeaders = true
			}
			upReq.Header.Del(name)
		}
	}

	if clientIP, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		// If we aren't the first proxy, retain prior X-Forwarded-For
		// information as a comma+space separated list and fold
		// multiple headers into one.
		if prior, ok := upReq.Header["X-Forwarded-For"]; ok {
			clientIP = strings.Join(prior, ", ") + ", " + clientIP
		}
		upReq.Header.Set("X-Forwarded-For", clientIP)
	}

	proxy.setVia(upReq.Header, req.Proto)

	// TODO(voss): what to do about pre-existing If-None-Match and
	//     If-Modified-Since headers?
	var lastModified time.Time
	for _, resp := range stale {
		etag := resp.Header.Get("Etag")
		if etag != "" {
			upReq.Header.Add("If-None-Match", etag)
		}
		lm := parseDate(resp.Header.Get("Last-Modified"))
		if lm.After(lastModified) {
			lastModified = lm
		}
	}
	if req.Method == "GET" || req.Method == "HEAD" {
		if !lastModified.IsZero() {
			upReq.Header.Set("If-Modified-Since",
				lastModified.Format(time.RFC1123))
		}
	}

	// requestTime := time.Now()
	upResp, err := proxy.upstream.RoundTrip(upReq)
	responseTime := time.Now()
	if err != nil {
		// TODO(voss): serve stale responses if available?
		trace.T("jvproxy/handler", trace.PrioDebug,
			"upstream server request failed: %s", err.Error())
		msg := "error: " + err.Error()
		h := http.Header{}
		h.Add("Content-Type", "text/plain")
		return &proxyResponse{ // TODO(voss)
			StatusCode: 555,
			Header:     h,
			source:     "error",
			body:       ioutil.NopCloser(strings.NewReader(msg)),
		}
	}

	if upResp.StatusCode == http.StatusNotModified {
		selected := []*proxyResponse{}
		done := false

		eTag1 := upResp.Header.Get("Etag")
		lastModified1 := upResp.Header.Get("Last-Modified")
		lm := parseDate(lastModified1)

		// RFC 7234, section 4.3.4a: If the new response contains a
		// strong validator (see Section 2.1 of [RFC7232]), then that
		// strong validator identifies the selected representation for
		// update.  All of the stored responses with the same strong
		// validator are selected.  If none of the stored responses
		// contain the same strong validator, then the cache MUST NOT
		// use the new response to update any stored responses.
		if eTag1 != "" && !strings.HasPrefix(eTag1, "W/") {
			for _, resp := range stale {
				eTag2 := resp.Header.Get("Etag")
				if eTag1 == eTag2 {
					selected = append(selected, resp)
				}
			}
			done = true
		}
		if !done && !lm.IsZero() {
			// RFC 7232, section-2.2.2b: [A Last-Modified header can
			// be used as a strong validator, if the] cache entry
			// includes a Date value, which gives the time when the
			// origin server sent the original response, and [the]
			// presented Last-Modified time is at least 60 seconds
			// before the Date value.
			for _, resp := range stale {
				date := parseDate(resp.Header.Get("Date"))
				if !date.IsZero() && date.Sub(lm) >= 60*time.Second {
					selected = append(selected, resp)
					done = true
				}
			}
		}

		// RFC 7234, section 4.3.4b: If the new response contains a
		// weak validator and that validator corresponds to one of the
		// cache's stored responses, then the most recent of those
		// matching stored responses is selected for update.
		if !done && eTag1 != "" {
			if !strings.HasPrefix(eTag1, "W/") {
				panic("something went wrong")
			}
			eTag1 = eTag1[2:]
			for _, resp := range stale {
				eTag2 := resp.Header.Get("Etag")
				if strings.HasPrefix(eTag2, "W/") {
					eTag2 = eTag2[2:]
				}
				if eTag1 == eTag2 {
					selected = append(selected, resp)
				}
			}
			if len(selected) > 0 {
				selected = selected[:1]
				done = true
			}
		}
		if !done && !lm.IsZero() {
			for _, resp := range stale {
				lastModified2 := resp.Header.Get("Last-Modified")
				if lastModified1 == lastModified2 {
					selected = append(selected, resp)
				}
			}
			if len(selected) > 0 {
				selected = selected[:1]
				done = true
			}
		}

		// RFC 7234, section 4.3.4c: If the new response does not
		// include any form of validator (such as in the case where a
		// client generates an If-Modified-Since request from a source
		// other than the Last-Modified response header field), and
		// there is only one stored response, and that stored response
		// also lacks a validator, then that stored response is
		// selected for update.
		if !done && eTag1 == "" && len(stale) == 1 &&
			stale[0].Header.Get("Last-Modified") == "" {
			selected = stale
			done = true
		}
	}

	// Fix upstream-provided headers as required for forwarding and
	// storage.
	for _, name := range perHopHeaders {
		upResp.Header.Del(name)
	}
	proxy.setVia(upResp.Header, upResp.Proto)
	if len(upResp.Header["Date"]) == 0 {
		upResp.Header.Set("Date", responseTime.Format(time.RFC1123))
	}

	return &proxyResponse{
		StatusCode: upResp.StatusCode,
		Header:     upResp.Header,
		source:     "upstream",
		body:       upResp.Body,
	}
}

func (proxy *Proxy) setVia(header http.Header, proto string) {
	via := proto + " " + proxy.Name + " (jvproxy)"
	if strings.HasPrefix(via, "HTTP/") {
		via = via[5:]
	}
	if prior, ok := header["Via"]; ok {
		via = strings.Join(prior, ", ") + ", " + via
	}
	header.Set("Via", via)
}

type decision struct {
	canServeFromCache bool
	canStore          bool
	hasAuthorization  bool
	mustRevalidate    bool
	log               []string
}

// first result: can use cache for response
// second result: can store server response in cache
func (proxy *Proxy) getCacheability(req *http.Request) *decision {
	res := &decision{}

	headers := req.Header
	cc, _ := parseHeaders(headers["Cache-Control"])

	// RFC 7234, section 3
	res.canStore = true

	if req.Method != "GET" && req.Method != "HEAD" {
		// TODO(voss): handle more method types
		res.canStore = false
		res.log = append(res.log, "req:method="+req.Method)
	}

	if _, hasNoStore := cc["no-store"]; hasNoStore {
		res.canStore = false
		res.log = append(res.log, "req:CC=NS")
	}

	if proxy.shared && len(headers["Authorization"]) > 0 {
		res.hasAuthorization = true
		// decision defered to `proxy.updateCacheability()`
	}

	// RFC 7234, section 4
	res.canServeFromCache = true

	pragma, _ := parseHeaders(headers["Pragma"])
	if _, hasNoCache := pragma["no-cache"]; hasNoCache {
		res.mustRevalidate = true
		res.log = append(res.log, "req:P=NC")
	}

	if _, hasNoCache := cc["no-cache"]; hasNoCache {
		res.mustRevalidate = true
		res.log = append(res.log, "req:CC=NC")
	}

	return res
}

func (proxy *Proxy) updateCacheability(resp *proxyResponse, res *decision) {
	// At this point we already have obtained a new response from the
	// server: only the .canStore field is still interesting.

	// RFC 7234, section 3
	if !res.canStore {
		return
	}

	headers := resp.Header
	cc, _ := parseHeaders(headers["Cache-Control"])

	switch resp.StatusCode {
	case 200, 203, 204, 300, 301, 404, 405, 410, 414, 501:
		// status codes understood by the proxy

		// pass
	default:
		// This currently includes 206 (partial content)
		res.canStore = false
		res.log = append(res.log, "resp:code="+strconv.Itoa(resp.StatusCode))
	}

	if _, hasNoStore := cc["no-store"]; hasNoStore {
		res.canStore = false
		res.log = append(res.log, "resp:CC=NS")
	}

	if _, hasPrivate := cc["private"]; proxy.shared && hasPrivate {
		res.canStore = false
		res.log = append(res.log, "resp:CC=P")
	}

	if res.hasAuthorization {
		_, ok1 := cc["must-revalidate"]
		_, ok2 := cc["public"]
		_, ok3 := cc["s-maxage"]
		if !(ok1 || ok2 || ok3) {
			res.canStore = false
			res.log = append(res.log, "resp:Auth")
		}
	}

	cacheable := false
	if len(headers["Expires"]) > 0 {
		cacheable = true
	}
	if _, hasMaxAge := cc["max-age"]; hasMaxAge {
		cacheable = true
	}
	if _, hasSMaxage := cc["s-maxage"]; proxy.shared && hasSMaxage {
		cacheable = true
	}
	switch resp.StatusCode {
	case 200, 203, 204, 206, 300, 301, 404, 405, 410, 414, 501:
		// status codes defined as cacheable by default
		cacheable = true
	}
	if _, hasPublic := cc["public"]; hasPublic {
		cacheable = true
	}
	res.canStore = res.canStore && cacheable
}
