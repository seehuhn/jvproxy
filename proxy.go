package jvproxy

import (
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/seehuhn/httputil"
	"github.com/seehuhn/jvproxy/cache"
	"github.com/seehuhn/trace"
)

type Proxy struct {
	Name     string
	upstream http.RoundTripper
	cache    cache.Cache
	logger   chan<- *LogEntry
	AdminMux *http.ServeMux
	shared   bool
}

func NewProxy(name string, transport http.RoundTripper, cache cache.Cache, shared bool) *Proxy {
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
		RequestTime: requestTime,
		RemoteAddr:  req.RemoteAddr,
		Method:      req.Method,
		RequestURI:  req.RequestURI,
	}
	defer func() {
		log.HandlerCompleteNano =
			int64(time.Since(requestTime) / time.Nanosecond)
		proxy.logger <- log
	}()

	if req.Method == "CONNECT" {
		dest := req.Host

		hj, ok := w.(http.Hijacker)
		if !ok {
			trace.T("jvproxy/handler", trace.PrioError,
				"cannot hijack connection for tunnel to %q",
				dest)
			code := http.StatusInternalServerError
			http.Error(w, http.StatusText(code), code)
			log.StatusCode = code
			return
		}

		// check that we can resolve the server in the DNS
		destAddr, err := net.ResolveTCPAddr("tcp", dest)
		if err != nil {
			trace.T("jvproxy/handler", trace.PrioInfo,
				"tunnel to %q on behalf of %s failed: %s",
				dest, req.RemoteAddr, err.Error())
			code := http.StatusNotFound
			http.Error(w, http.StatusText(code), code)
			log.StatusCode = code
			return
		}
		if destAddr.Port != 80 && destAddr.Port != 443 &&
			!strings.HasPrefix(req.RemoteAddr, "127.0.0.1:") {
			trace.T("jvproxy/handler", trace.PrioInfo,
				"tunnel to %s on behalf of %s not allowed",
				destAddr, req.RemoteAddr)
			code := http.StatusForbidden
			http.Error(w, http.StatusText(code), code)
			log.StatusCode = code
			return
		}

		destConn, err := net.DialTCP("tcp", nil, destAddr)
		if err != nil {
			trace.T("jvproxy/handler", trace.PrioInfo,
				"error while connecting to %s for tunnel: %s",
				destAddr, err.Error())
			code := http.StatusNotFound
			http.Error(w, http.StatusText(code), code)
			log.StatusCode = code
			return
		}
		defer destConn.Close()
		destConn.SetLinger(60)

		conn, client, err := hj.Hijack()
		if err != nil {
			trace.T("jvproxy/handler", trace.PrioDebug,
				"error while setting up tunnel on behalf of %s: %s",
				req.RemoteAddr, err.Error())
			code := http.StatusInternalServerError
			http.Error(w, http.StatusText(code), code)
			log.StatusCode = code
			return
		}
		defer conn.Close()

		// From this point on, the usual http methods on `w` won't
		// work any more.

		var zeroTime time.Time
		conn.SetDeadline(zeroTime)

		trace.T("jvproxy/handler", trace.PrioDebug,
			"created tunnel to %s on behalf of %q",
			destAddr, req.RemoteAddr)
		client.WriteString("HTTP/1.1 200 OK\r\n\r\n")
		client.Flush()
		tunnel(destConn, conn)

		return
	}

	cacheInfo := proxy.getCacheability(req)

	var respData *cache.Entry

	// step 1: check whether any cached responses are available
	choices := []*cache.Entry{}
	if cacheInfo.canServeFromCache {
		choices = proxy.cache.Retrieve(req)
		if len(choices) > 0 {
			sort.Sort(byDate(choices))

			// TODO(voss): is the following what we want?  The code in
			// .requestFromUpstream() seems prepared for there to be
			// more than one stale response.
			respData = choices[0]
		}
	}

	// step 2: if the responses are stale, send a validation request
	if respData != nil {
		freshnessLifetime := proxy.getFreshnessLifetime(respData)
		currentAge := proxy.getCurrentAge(respData)
		stale := freshnessLifetime <= currentAge
		// TODO(voss): revalidate, if the cached response contains the
		// no-cache directive
		if stale || cacheInfo.mustRevalidate {
			log.CacheResult += "REVALIDATE,"
			respData = proxy.requestFromUpstream(req, choices)
		}
	}

	// step 3: make sure we still have the body of the selected response
	var body io.ReadCloser
	if respData != nil {
		body = respData.GetBody()
		if body == nil {
			log.CacheResult += "DROPPED,"
			respData = nil
		}
	}

	// step 4: if the above fails, forward the request upstream
	isHit := respData != nil
	if isHit {
		log.CacheResult += "HIT"
		cacheInfo.canStore = false
	} else {
		log.CacheResult += "MISS"
		respData = proxy.requestFromUpstream(req, nil)
	}

	log.ResponseReceivedNano = int64(time.Since(requestTime) / time.Nanosecond)
	log.StatusCode = respData.StatusCode

	proxy.updateCacheability(respData, cacheInfo)
	log.Comments = append(log.Comments, cacheInfo.log...)

	h := w.Header()
	copyHeader(h, respData.Header)
	w.WriteHeader(respData.StatusCode)

	if body == nil {
		body = respData.GetBody()
	}
	// TODO(voss): retry if body==nil ?
	defer body.Close()

	var n int64
	var err error
	if cacheInfo.canStore {
		entry := proxy.cache.StoreStart(req.URL.String(), &respData.MetaData)
		n, err = io.Copy(w, entry.Reader(body))
		if err != nil {
			entry.Discard()
		} else {
			entry.Commit(n)
		}
		log.CacheResult += ",STORE"
	} else {
		n, err = io.Copy(w, body)
		if !isHit {
			log.CacheResult += ",NOSTORE"
		}
	}
	if err != nil {
		trace.T("jvproxy/handler", trace.PrioDebug,
			"error while writing response: %s", err.Error())
	}
	log.ContentLength = n
	// TODO(voss): compare n to the server-provided Content-Length?
	// Or maybe unconditionally add a Content-Length header to the
	// cached version?
}

type byDate []*cache.Entry

func (x byDate) Len() int      { return len(x) }
func (x byDate) Swap(i, j int) { x[i], x[j] = x[j], x[i] }
func (x byDate) Less(i, j int) bool {
	dateI := httputil.ParseDate(x[i].Header.Get("Date"))
	dateJ := httputil.ParseDate(x[i].Header.Get("Date"))
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
func (proxy *Proxy) requestFromUpstream(req *http.Request, stale []*cache.Entry) *cache.Entry {
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
	conditional := false
	var lastModified time.Time
	for _, resp := range stale {
		etag := resp.Header.Get("Etag")
		if etag != "" {
			upReq.Header.Add("If-None-Match", etag)
			conditional = true
		}
		lm := httputil.ParseDate(resp.Header.Get("Last-Modified"))
		if lm.After(lastModified) {
			lastModified = lm
		}
	}
	if req.Method == "GET" || req.Method == "HEAD" {
		if !lastModified.IsZero() {
			upReq.Header.Set("If-Modified-Since",
				lastModified.Format(time.RFC1123))
			conditional = true
		}
	}

	requestTime := time.Now()
	upResp, err := proxy.upstream.RoundTrip(upReq)
	responseTime := time.Now()
	if err != nil {
		// TODO(voss): serve stale responses if available?
		trace.T("jvproxy/handler", trace.PrioDebug,
			"upstream server request failed: %s %s: %s",
			req.Method, req.RequestURI, err.Error())
		msg := "error: " + err.Error()
		h := http.Header{}
		h.Add("Content-Type", "text/plain")
		// TODO(voss): use the correct 502/504 responses.
		return &cache.Entry{ // TODO(voss): invent error reporting mechanism
			MetaData: cache.MetaData{
				StatusCode: 555,
				Header:     h,
			},
			Source: "error",
			GetBody: func() io.ReadCloser {
				return ioutil.NopCloser(strings.NewReader(msg))
			},
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

	if conditional && upResp.StatusCode == http.StatusNotModified {
		selected := []*cache.Entry{}
		done := false

		eTag1 := upResp.Header.Get("Etag")
		lastModified1 := upResp.Header.Get("Last-Modified")
		lm := httputil.ParseDate(lastModified1)

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
				date := httputil.ParseDate(resp.Header.Get("Date"))
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

		if len(selected) > 0 {
			// RFC 7234, section 4.3.4: If a stored response is
			// selected for update, the cache MUST:
			for _, entry := range selected {
				// RFC 7234, section 4.3.4d: delete any Warning header
				// fields in the stored response with warn-code 1xx;
				warn := entry.Header["Warning"]
				i := 0
				for i < len(warn) {
					if strings.HasPrefix(warn[i], "1") {
						warn = append(warn[:i], warn[i+1:]...)
					} else {
						i++
					}
				}

				// RFC 7234, section 4.3.4f: use other header fields
				// provided in the 304 (Not Modified) response to
				// replace all instances of the corresponding header
				// fields in the stored response.
				//
				// TODO(voss): what is "other"?
				for key, val := range upResp.Header {
					entry.Header[key] = val
				}

				entry.ResponseTime = responseTime
				entry.ResponseDelay = responseTime.Sub(requestTime)
				proxy.cache.Update(req.URL.String(), entry)
			}

			sort.Sort(byDate(selected))
			return selected[0]
		}
		return nil
	}

	return &cache.Entry{
		MetaData: cache.MetaData{
			StatusCode:    upResp.StatusCode,
			Header:        upResp.Header,
			ResponseTime:  responseTime,
			ResponseDelay: responseTime.Sub(requestTime),
		},
		Source:  "upstream",
		GetBody: func() io.ReadCloser { return upResp.Body },
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
		// TODO(voss): does the comment above still make sense?
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

func (proxy *Proxy) updateCacheability(resp *cache.Entry, res *decision) {
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
	if !cacheable {
		res.canStore = false
		res.log = append(res.log, "resp:nc")
	}
}
