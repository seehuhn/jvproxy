package main

import (
	"crypto/sha256"
	"encoding/gob"
	"github.com/seehuhn/trace"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type Proxy struct {
	store     *Store
	reportMux *http.ServeMux
	tokens    chan bool
}

func NewProxy(cacheDir string, reportMux *http.ServeMux) (*Proxy, error) {
	store, err := NewStore(cacheDir)
	if err != nil {
		return nil, err
	}

	maxParallel := 1000 // TODO(voss): do we need a limit here?  which one?
	tokens := make(chan bool, maxParallel)
	for i := 0; i < maxParallel; i++ {
		tokens <- true
	}

	return &Proxy{
		store:     store,
		reportMux: reportMux,
		tokens:    tokens,
	}, nil
}

func (proxy *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Host == "" || r.URL.Host == listenAddr {
		proxy.reportMux.ServeHTTP(w, r)
		return
	}

	requestTime := time.Now()
	log := &logEntry{
		RequestTimeNano: requestTime.UnixNano(),
		RemoteAddr:      r.RemoteAddr,
		Method:          r.Method,
		RequestURI:      r.RequestURI,
	}
	token := <-proxy.tokens
	defer func() {
		log.HandlerDurationNano =
			int64(time.Since(requestTime) / time.Nanosecond)
		proxy.store.index.Insert(log)
		proxy.tokens <- token
	}()

	via := r.Proto + " jvproxy"
	if strings.HasPrefix(via, "HTTP/") {
		via = via[5:]
	}

	var err error
	var entry *indexEntry

	canServeFromCache, canStoreInCache := canUseCache(r.Method, r.Header, log)
	hash := sha256.Sum224([]byte(r.URL.String()))
	if canServeFromCache {
		entry, err = proxy.store.Lookup(hash[:], r.Header, log)
		if err != nil {
			trace.T("jvproxy/handler", trace.PrioDebug,
				"cache lookup failed: %s", err.Error())
			http.Error(w, err.Error(), 500)
			return
		}
	}

	if entry != nil {
		fname := proxy.store.fileName(entry.Id)
		f, err := os.Open(fname + "h")
		dec := gob.NewDecoder(f)
		header := w.Header()
		e2 := dec.Decode(&header)
		if err == nil {
			err = e2
		}
		e2 = f.Close()
		if err == nil {
			err = e2
		}
		if err != nil {
			trace.T("jvproxy/handler", trace.PrioDebug,
				"error while reading headers from cache: %s", err.Error())
			http.Error(w, err.Error(), 500)
			return
		}
		header.Add("Via", via)
		w.WriteHeader(entry.StatusCode)

		f, err = os.Open(fname)
		n, e2 := io.Copy(w, f)
		if err == nil {
			err = e2
		}
		e2 = f.Close()
		if err == nil {
			err = e2
		}
		if err != nil {
			trace.T("jvproxy/handler", trace.PrioDebug,
				"error while reading headers from cache: %s", err.Error())
			// TODO(voss): what to do here?
		}
		proxy.store.Complete(entry)

		log.CacheResult = "HIT"
		log.StatusCode = entry.StatusCode
		log.ContentLength = n
	} else {
		r.Header.Add("Via", via)
		resp, err := client.RoundTrip(r)
		responseTime := time.Now()
		if err != nil {
			trace.T("jvproxy/handler", trace.PrioDebug,
				"upstream server request failed: %s", err.Error())
			http.Error(w, err.Error(), 500)
			return
		}

		log.StatusCode = resp.StatusCode
		switch resp.StatusCode {
		case 200, 203, 300, 301, 410:
			// pass
		case 206:
			// Range and Content-Range not implemented yet
			canStoreInCache = false
		default:
			canStoreInCache = false
		}

		cc, err := parseHeaders(resp.Header["Cache-Control"])
		if err != nil {
			trace.T("jvproxy/handler", trace.PrioDebug,
				"cannot parse response Cache-Control directive: %s", err.Error())
		}

		_ = cc

		expires := parseDate(resp.Header.Get("Expires"))
		if expires.IsZero() {
			// http://tools.ietf.org/html/rfc7234#section-4.2.3
			age, _ := strconv.Atoi(resp.Header.Get("Age"))
			ageValue := time.Duration(age) * time.Second
			dateValue := parseDate(resp.Header.Get("Date"))

			apparentAge := responseTime.Sub(dateValue)
			if apparentAge < 0*time.Second {
				apparentAge = 0 * time.Second
			}
			responseDelay := responseTime.Sub(requestTime)
			correctedAgeValue := ageValue + responseDelay

			correctedInitialAge := apparentAge
			if correctedAgeValue > correctedInitialAge {
				correctedInitialAge = correctedAgeValue
			}

			// fmt.Println("apparentCreationDate = ",
			//	responseTime.Add(-correctedInitialAge))
		}

		h := w.Header()
		for key, values := range resp.Header {
			for _, val := range values {
				h.Add(key, val)
			}
		}
		// TODO(voss): remove per-hop headers
		h.Add("Via", via)

		if canStoreInCache {
			// store an empty index entry to reserve a unique ID
			res, err := proxy.store.index.Exec(
				"INSERT INTO \"index\" DEFAULT VALUES")
			if err != nil {
				trace.T("jvproxy/handler", trace.PrioDebug,
					"error while reserving index entry: %s", err.Error())
				http.Error(w, err.Error(), 500)
				return
			}
			rowId, err := res.LastInsertId()
			if err != nil {
				panic(err)
			}
			fname := proxy.store.fileName(rowId)

			f, err := os.Create(fname + "h")
			enc := gob.NewEncoder(f)
			e2 := enc.Encode(resp.Header)
			if err == nil {
				err = e2
			}
			e2 = f.Close()
			if err == nil {
				err = e2
			}
			if err != nil {
				trace.T("jvproxy/handler", trace.PrioDebug,
					"error while storing response headers in the cache: %s",
					err.Error())
				http.Error(w, err.Error(), 500)
				return
			}

			f, err = os.Create(fname + "c")
			enc = gob.NewEncoder(f)
			e2 = enc.Encode(r.Header)
			if err == nil {
				err = e2
			}
			e2 = f.Close()
			if err == nil {
				err = e2
			}
			if err != nil {
				trace.T("jvproxy/handler", trace.PrioDebug,
					"error while storing request headers in the cache: %s",
					err.Error())
				http.Error(w, err.Error(), 500)
				return
			}

			f, err = os.Create(fname)
			if err != nil {
				trace.T("jvproxy/handler", trace.PrioDebug,
					"error while storing new entry in the DB: %s", err.Error())
				http.Error(w, err.Error(), 500)
				return
			}

			w.WriteHeader(resp.StatusCode)
			n, err := io.Copy(w, io.TeeReader(resp.Body, f))
			e2 = f.Close()
			if err == nil {
				err = e2
			}
			if err != nil {
				trace.T("jvproxy/handler", trace.PrioDebug,
					"error while storing file in cache: %s", err.Error())
			}
			// TODO(voss): compare n to the server-provided Content-Length

			entry := &indexEntry{}
			entry.Id = rowId
			entry.Hash = hash[:]
			entry.Vary = strings.Replace(
				strings.Join(resp.Header["Vary"], ","), " ", "", -1)
			if entry.Vary != "" {
				entry.VaryHash = hashVaryInformation(
					strings.Split(entry.Vary, ","), r.Header)
			}
			entry.StatusCode = resp.StatusCode
			// ModTime
			entry.DownloadTimeNano = responseTime.UnixNano()
			entry.ExpiryTime = expires.Unix()
			// LastUsedTime
			// ETag
			entry.ContentLength = n
			_, err = proxy.store.index.Update(entry)
			if err != nil {
				trace.T("jvproxy/handler", trace.PrioDebug,
					"error while storing new entry in the DB: %s", err.Error())
			}

			log.CacheResult = "MISS,STORE"
			log.ContentLength = n
		} else {
			w.WriteHeader(resp.StatusCode)
			io.Copy(w, resp.Body)

			log.CacheResult = "MISS,NOSTORE"
		}

		resp.Body.Close()
	}
}
