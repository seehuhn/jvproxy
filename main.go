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
	"flag"
	"fmt"
	"github.com/coopernurse/gorp"
	_ "github.com/mattn/go-sqlite3" // we use the sqlite3 backend for gorp
	"github.com/seehuhn/trace"
	"log"
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
	Id               int64
	Hash             []byte // hash of the request url
	Vary             string // comma-separated Vary fields of the response
	VaryHash         []byte // hash of the response Vary information
	StatusCode       int
	ModTime          int64
	DownloadTimeNano int64
	ExpiryTime       int64
	LastUsedTime     int64 // time of last access (seconds since Jan 1, 1970, UTC)
	ETag             string
	ContentLength    int64
}

type Store struct {
	baseDir string
	index   *gorp.DbMap
}

var SQLdebug = flag.Bool("sqldebug", false,
	"print all SQL statements as they are executed")

func NewStore(baseDir string) (*Store, error) {
	s := &Store{
		baseDir: baseDir,
	}
	err := s.setup()

	openUrl :=
		"file:" + filepath.Join(baseDir, "index.sqlite?cache=shared&mode=rwc")
	trace.T("main/db", trace.PrioInfo, "opening sqlite3 database %q", openUrl)
	db, err := sql.Open("sqlite3", openUrl)
	if err != nil {
		return nil, err
	}
	dbmap := &gorp.DbMap{Db: db, Dialect: gorp.SqliteDialect{}}
	if *SQLdebug {
		dbmap.TraceOn("[gorp]",
			log.New(os.Stdout, "SQL:", log.Lmicroseconds))
	}
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
		"SELECT * FROM \"index\" WHERE Hash=? ORDER BY LENGTH(Vary)", urlHash)
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
		varyHeaders := strings.Split(entry.Vary, ",")
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
	// Proxy:                 seehuhnProxy,
	TLSHandshakeTimeout:   10 * time.Second,
	ResponseHeaderTimeout: 10 * time.Second,
}

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

func main() {
	flag.Parse()

	proxy, err := NewProxy("cache", reportMux)
	if err != nil {
		trace.T("jvproxy/main", trace.PrioCritical,
			"cannot open store: %s", err.Error())
		fmt.Fprintf(os.Stderr, "error: cannot open store, %s\n", err.Error())
		os.Exit(1)
	}

	installReport("summary", proxy.store.summaryHandler)
	installReport("log", proxy.store.logHandler)
	installReport("store", proxy.store.storeHandler)
	installReport("highlights", proxy.store.highlightsHandler)
	installReport("variants", proxy.store.variantsHandler)
	reportMux.Handle("/css/",
		http.StripPrefix("/css/", http.FileServer(http.Dir("css"))))
	reportMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
		} else {
			http.Redirect(w, r, "/summary", http.StatusMovedPermanently)
		}
	})

	server := &http.Server{
		Addr:         listenAddr,
		Handler:      proxy,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	trace.T("main", trace.PrioInfo, "listening at %q", listenAddr)
	server.ListenAndServe()
}
