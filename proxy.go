package main

import (
	"github.com/seehuhn/trace"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"time"
)

type Proxy struct {
	name    string
	upTrans http.RoundTripper
	cache   Cache
	logger  chan<- *LogEntry
}

func NewProxy(name string, transport http.RoundTripper, cache Cache) *Proxy {
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &Proxy{
		name:    name,
		upTrans: transport,
		cache:   cache,
		logger:  NewLogger(),
	}
}

func (proxy *Proxy) Close() error {
	return proxy.cache.Close()
}

func (proxy *Proxy) ServeHTTP(w http.ResponseWriter, req *http.Request) {
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

	canServeFromCache, canStoreInCache := canUseCache(
		req.Method, req.Header, log)

	var respData *proxyResponse

	if canServeFromCache {
		respData = proxy.cache.Retrieve(req)
	}
	if respData != nil {
		log.CacheResult = "HIT"
		canStoreInCache = false
	} else {
		respData = proxy.requestFromUpstream(req)
	}
	log.ResponseReceivedNano = int64(time.Since(requestTime) / time.Nanosecond)
	log.StatusCode = respData.StatusCode

	if canStoreInCache {
		canStoreInCache = canStoreResponse(
			respData.StatusCode, respData.Header, log)
	}

	h := w.Header()
	copyHeader(h, respData.Header)
	w.WriteHeader(respData.StatusCode)
	var n int64
	var err error
	if canStoreInCache {
		entry := proxy.cache.StoreStart(respData.StatusCode, respData.Header)
		n, err = io.Copy(w, io.TeeReader(respData.Body, entry.Body()))
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

	upResp, err := proxy.upTrans.RoundTrip(upReq)
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

	for _, name := range perHopHeaders {
		upResp.Header.Del(name)
	}
	proxy.setVia(upResp.Header, upResp.Proto)

	return &proxyResponse{
		StatusCode: upResp.StatusCode,
		Header:     upResp.Header,
		Body:       upResp.Body,
	}
}

func (proxy *Proxy) setVia(header http.Header, proto string) {
	via := proto + " " + proxy.name + " (jvproxy)"
	if strings.HasPrefix(via, "HTTP/") {
		via = via[5:]
	}
	if prior, ok := header["Via"]; ok {
		via = strings.Join(prior, ", ") + ", " + via
	}
	header.Set("Via", via)
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
