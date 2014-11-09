package main

import (
	"github.com/seehuhn/trace"
	"io"
	"io/ioutil"
	"net"
	"net/http"
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

	if cacheInfo.canServeFromCache {
		respData = proxy.cache.Retrieve(req)
	}
	if respData != nil {
		log.CacheResult = "HIT"
		cacheInfo.canStore = false
	} else {
		respData = proxy.requestFromUpstream(req)
	}
	log.ResponseReceivedNano = int64(time.Since(requestTime) / time.Nanosecond)
	log.StatusCode = respData.StatusCode

	proxy.updateCacheability(respData, cacheInfo)
	log.Comments = append(log.Comments, cacheInfo.log...)

	h := w.Header()
	copyHeader(h, respData.Header)
	w.WriteHeader(respData.StatusCode)
	var n int64
	var err error
	if cacheInfo.canStore {
		entry := proxy.cache.StoreStart(
			req.URL.String(), respData.StatusCode, respData.Header)
		n, err = io.Copy(w, entry.Reader(respData.Body))
		if err != nil {
			entry.Abort()
		} else {
			entry.Complete()
		}
		log.CacheResult = "MISS,STORE"
	} else {
		n, err = io.Copy(w, respData.Body)
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

	respData.Body.Close()
}

type proxyResponse struct {
	StatusCode int
	Header     http.Header
	Body       io.ReadCloser
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

func (proxy *Proxy) requestFromUpstream(req *http.Request) *proxyResponse {
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

	// requestTime := time.Now()
	upResp, err := proxy.upstream.RoundTrip(upReq)
	responseTime := time.Now()
	if err != nil {
		trace.T("jvproxy/handler", trace.PrioDebug,
			"upstream server request failed: %s", err.Error())
		msg := "error: " + err.Error()
		h := http.Header{}
		h.Add("Content-Type", "text/plain")
		return &proxyResponse{ // TODO(voss)
			StatusCode: 555,
			Header:     h,
			Body:       ioutil.NopCloser(strings.NewReader(msg)),
		}
	}

	// Fix upstream-provided headers as required for forwarding and
	// storage.
	for _, name := range perHopHeaders {
		upResp.Header.Del(name)
	}
	proxy.setVia(upResp.Header, upResp.Proto)
	if len(upResp.Header["Date"]) == 0 {
		upResp.Header.Set("Date",
			responseTime.Format(time.RFC1123))
	}

	return &proxyResponse{
		StatusCode: upResp.StatusCode,
		Header:     upResp.Header,
		Body:       upResp.Body,
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
		// TODO(voss): handle POST?
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
