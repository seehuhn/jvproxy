// main.go - main file of the jvproxy caching web proxy
// Copyright (C) 2014  Jochen Voss <voss@seehuhn.de>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/gob"
	"flag"
	"fmt"
	"github.com/coopernurse/gorp"
	_ "github.com/mattn/go-sqlite3" // we use the sqlite3 backend for gorp
	"github.com/seehuhn/trace"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const listenAddr = "localhost:8080"
const listenAddrTLS = "localhost:8081"

type logEntry struct {
	Id                  int64
	RequestTimeNano     int64
	RemoteAddr          string
	Method              string
	RequestURI          string
	StatusCode          int
	ContentLength       int64
	CacheResult         string
	Comment             string
	HandlerDurationNano int64
}

type indexEntry struct {
	Id            int64
	Hash          []byte // hash of the request url
	Vary          string // colon-separated Vary fields of the response
	VaryHash      []byte // hash of the response Vary information
	StatusCode    int
	ModTime       int64
	DownLoadTime  int64
	ExpiryTime    int64
	LastUsedTime  int64 // time of last access (seconds since Jan 1, 1970, UTC)
	ETag          string
	ContentLength int64
}

type Store struct {
	baseDir string
	index   *gorp.DbMap
}

func NewStore(baseDir string) (*Store, error) {
	s := &Store{
		baseDir: baseDir,
	}
	err := s.setup()

	db, err := sql.Open("sqlite3", filepath.Join(baseDir, "index.db"))
	if err != nil {
		return nil, err
	}
	dbmap := &gorp.DbMap{Db: db, Dialect: gorp.SqliteDialect{}}
	dbmap.AddTableWithName(indexEntry{}, "index").SetKeys(true, "Id")
	dbmap.AddTableWithName(logEntry{}, "log").SetKeys(true, "Id")
	err = dbmap.CreateTablesIfNotExists()
	if err != nil {
		return nil, err
	}
	s.index = dbmap

	return s, err
}

func (s *Store) setup() error {
	didCreate := false
	err := os.Mkdir(s.baseDir, 0755)
	if err != nil && !os.IsExist(err) {
		return err
	}
	didCreate = didCreate || (err == nil)
	for i := 0; i < 100; i++ {
		part := fmt.Sprintf("%02d", i)
		err = os.Mkdir(filepath.Join(s.baseDir, part), 0755)
		if err != nil && !os.IsExist(err) {
			return err
		}
		didCreate = didCreate || (err == nil)
	}
	if didCreate {
		trace.T("jvproxy/store", trace.PrioInfo,
			"created store directories under %s", s.baseDir)
	}
	return nil
}

func (s *Store) Close() error {
	return s.index.Db.Close()
}

func (s *Store) fileName(id int64) string {
	return filepath.Join(s.baseDir,
		fmt.Sprintf("%02d", id%100), fmt.Sprintf("%015d", id/100))
}

func (s *Store) Lookup(urlHash []byte, header http.Header,
	log *logEntry) (res *indexEntry, err error) {
	entries := []indexEntry{}
	_, err = s.index.Select(&entries,
		"SELECT * FROM `index` WHERE Hash=? ORDER BY LENGTH(Vary)", urlHash)
	if err != nil {
		return
	}

	if len(entries) > 0 {
		log.Comment += strconv.Itoa(len(entries)) + " variants "
	}

	for _, entry := range entries {
		if entry.Vary == "" {
			res = &entry
			break
		}
		varyHeaders := strings.Split(entry.Vary, ":")
		varyHash := hashVaryInformation(varyHeaders, header)
		if bytes.Compare(entry.VaryHash, varyHash) == 0 {
			res = &entry
			break
		}
	}
	return
}

func hashVaryInformation(varyHeaders []string, header http.Header) []byte {
	h := sha256.New224()
	for _, key := range varyHeaders {
		normalized := normalizeHeader(strings.Join(header[key], ","))
		h.Write([]byte(normalized))
		h.Write([]byte{0})
	}
	return h.Sum(nil)
}

func (s *Store) Complete(entry *indexEntry) {
	entry.LastUsedTime = time.Now().Unix()
	s.index.Update(entry)
}

func seehuhnProxy(r *http.Request) (*url.URL, error) {
	return url.Parse("http://vseehuhn.vpn.seehuhn.de:3128")
}

var client = &http.Transport{
	Proxy:                 seehuhnProxy,
	TLSHandshakeTimeout:   10 * time.Second,
	ResponseHeaderTimeout: 10 * time.Second,
}

var store *Store

func parseDate(dateStr string) time.Time {
	var t time.Time
	if dateStr != "" {
		for _, format := range []string{time.RFC1123, time.RFC850, time.ANSIC} {
			t, err := time.Parse(format, dateStr)
			if err == nil {
				return t
			}
		}
	}
	return t
}

func handler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Host == "" || r.URL.Host == listenAddr {
		statsMux.ServeHTTP(w, r)
		return
	}

	requestTime := time.Now()
	log := &logEntry{
		RequestTimeNano: requestTime.UnixNano(),
		RemoteAddr:      r.RemoteAddr,
		Method:          r.Method,
		RequestURI:      r.RequestURI,
	}
	defer func() {
		log.HandlerDurationNano =
			int64(time.Since(requestTime) / time.Nanosecond)
		store.index.Insert(log)
	}()

	via := r.Proto + " jvproxy"
	if strings.HasPrefix(via, "HTTP/") {
		via = via[5:]
	}

	var err error
	var entry *indexEntry

	hash := sha256.Sum224([]byte(r.URL.String()))
	canServeFromCache, canStoreInCache := canUseCache(r.Header, log)
	if canServeFromCache {
		entry, err = store.Lookup(hash[:], r.Header, log)
		if err != nil {
			trace.T("jvproxy/handler", trace.PrioDebug,
				"cache lookup failed: %s", err.Error())
			http.Error(w, err.Error(), 500)
			return
		}
	}

	if entry != nil {
		fname := store.fileName(entry.Id)
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
		store.Complete(entry)

		log.CacheResult = "HIT"
		log.StatusCode = entry.StatusCode
		log.ContentLength = n
		fmt.Println("CACHE HIT:", r.URL.String())
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

			fmt.Println("apparentCreationDate = ",
				responseTime.Add(-correctedInitialAge))
		}

		h := w.Header()
		for key, values := range resp.Header {
			for _, val := range values {
				h.Add(key, val)
			}
		}
		// TODO(voss): remove per-hop headers
		h.Add("Via", via)
		w.WriteHeader(resp.StatusCode)

		if canStoreInCache {
			// store an empty index entry to reserve a unique ID
			entry := &indexEntry{}
			err = store.index.Insert(entry)
			if err != nil {
				trace.T("jvproxy/handler", trace.PrioDebug,
					"error while creating new file: %s", err.Error())
				http.Error(w, err.Error(), 500)
				return
			}
			fname := store.fileName(entry.Id)

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
					"error while storing headers in the cache: %s", err.Error())
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

			entry.Vary = strings.Join(resp.Header["Vary"], ":")
			if entry.Vary != "" {
				entry.VaryHash = hashVaryInformation(resp.Header["Vary"],
					resp.Header)
			}
			entry.StatusCode = resp.StatusCode
			entry.ExpiryTime = expires.Unix()
			entry.ContentLength = n
			entry.Hash = hash[:]
			_, err = store.index.Update(entry)
			if err != nil {
				trace.T("jvproxy/handler", trace.PrioDebug,
					"error while storing new entry in the DB: %s", err.Error())
			}
			log.CacheResult = "MISS,STORE"
			log.ContentLength = n
			fmt.Println("CACHE MISS, STORE:", r.URL.String())
		} else {
			io.Copy(w, resp.Body)
			log.CacheResult = "MISS,NOSTORE"
			fmt.Println("CACHE MISS, NOSTORE:", r.URL.String())
		}

		resp.Body.Close()
	}
}

var statsMux = http.NewServeMux()

func main() {
	flag.Parse()

	var err error
	store, err = NewStore("cache")
	if err != nil {
		trace.T("jvproxy/main", trace.PrioCritical,
			"cannot open store: %s", err.Error())
		fmt.Fprintf(os.Stderr, "error: cannot open store, %s\n", err.Error())
		os.Exit(1)
	}

	logTmpl = template.Must(template.ParseFiles("tmpl/log.html"))

	statsMux.HandleFunc("/log", store.logHandler)
	statsMux.Handle("/", http.StripPrefix("/css", http.FileServer(http.Dir("css"))))

	go func() {
		server := &http.Server{
			Addr:         listenAddrTLS,
			Handler:      http.HandlerFunc(handler),
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
		}
		server.ListenAndServeTLS("cert.pem", "key.pem")
	}()

	server := &http.Server{
		Addr:         listenAddr,
		Handler:      http.HandlerFunc(handler),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	server.ListenAndServe()
}
