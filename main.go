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

// +build ignore

package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/seehuhn/jvproxy"
	"github.com/seehuhn/jvproxy/cache"
	"github.com/seehuhn/trace"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

var listenAddr = flag.String("listen-addr", "0.0.0.0:8080",
	"the address to listen on, in the form host:port")

var upstreamProxy = flag.String("upstream-proxy", "",
	"an upstream proxy to forward requests to")

const tmplDir = "tmpl"

var tmplFuncs = template.FuncMap{
	"FormatDate":          formatDate,
	"FormatDateNano":      formatDateNano,
	"FormatTimeDelta":     formatTimeDelta,
	"FormatTimeDeltaNano": formatTimeDeltaNano,
	"BytesToHex":          bytesToHex,
}

func formatDate(unix int64) template.HTML {
	s := "&mdash;"
	if unix > 0 {
		t := time.Unix(unix, 0)
		s = t.Format("2006-01-02&nbsp;15:04:05")
	}
	return template.HTML(s)
}

func formatDateNano(unixNano int64) template.HTML {
	t := time.Unix(0, unixNano)
	s := "&mdash;"
	if !t.IsZero() {
		s = t.Format("2006-01-02&nbsp;15:04:05.000")
	}
	return template.HTML(s)
}

func formatTimeDelta(unix int64) template.HTML {
	if unix <= 0 {
		return template.HTML("&mdash;")
	}
	return doFormatTimeDelta(time.Unix(unix, 0))
}

func formatTimeDeltaNano(unixNano int64) template.HTML {
	t := time.Unix(0, unixNano)
	return doFormatTimeDelta(t)
}

func doFormatTimeDelta(t time.Time) template.HTML {
	s := "&mdash;"
	if !t.IsZero() {
		d := t.Sub(time.Now())
		if d < 0 {
			s = "-"
			d = -d
		} else {
			s = ""
		}
		if d >= 24*time.Hour {
			hours := int(d / time.Hour)
			s += fmt.Sprintf("%dd%02dh", hours/24, hours%24)
		} else if d >= time.Hour {
			minutes := int(d / time.Minute)
			s += fmt.Sprintf("%dh%02dm", minutes/60, minutes%60)
		} else if d >= time.Minute {
			seconds := int(d / time.Second)
			s += fmt.Sprintf("%dm%02ds", seconds/60, seconds%60)
		} else if d > 0 {
			s += fmt.Sprintf("%.2fs", float64(d)/float64(time.Second))
		} else {
			s = "0"
		}
	}
	return template.HTML(s)
}

func bytesToHex(x []byte) template.HTML {
	return template.HTML(hex.EncodeToString(x))
}

var reportTmpl = map[string]*template.Template{}

func installReport(mux *http.ServeMux, name string, handler http.HandlerFunc) {
	tmpl := template.Must(template.New(name+".html").
		Funcs(tmplFuncs).ParseFiles(filepath.Join(tmplDir, name+".html"),
		filepath.Join(tmplDir, "head_frag.html")))
	reportTmpl[name] = tmpl
	url := "/" + name
	mux.HandleFunc(url, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != url {
			http.NotFound(w, r)
			return
		}
		handler(w, r)
	})
}

func installAdminHandlers(mux *http.ServeMux, proxy *jvproxy.Proxy, cache cache.Cache) {
	installReport(mux, "index", func(w http.ResponseWriter, r *http.Request) {
		err := reportTmpl["index"].Execute(w, map[string]interface{}{
			"proxy": proxy,
			"cache": cache,
		})
		if err != nil {
			trace.T("jvproxy/admin", trace.PrioError,
				"rendering summary data into template failed: %s", err.Error())
		}
	})
	mux.Handle("/css/",
		http.StripPrefix("/css/", http.FileServer(http.Dir("css"))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
		} else {
			http.Redirect(w, r, "/index", http.StatusMovedPermanently)
		}
	})
}

func main() {
	flag.Parse()

	transport := &http.Transport{
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	}
	if *upstreamProxy != "" {
		addr := *upstreamProxy
		if !strings.Contains(addr, "://") {
			addr = "http://" + addr
		}
		upstreamProxyURL, err := url.Parse(addr)
		if err != nil {
			log.Fatalf("invalid upstream proxy name %q", addr)
		}
		trace.T("main", trace.PrioInfo,
			"forwarding to proxy %q", upstreamProxyURL)
		transport.Proxy = func(*http.Request) (*url.URL, error) {
			return upstreamProxyURL, nil
		}
	}

	cache, err := cache.NewLevelDBCache("cache-root")
	if err != nil {
		log.Fatalf("cannot create cache: %s", err.Error())
	}
	proxy := jvproxy.NewProxy(*listenAddr, transport, cache, true)

	installAdminHandlers(proxy.AdminMux, proxy, cache)

	server := &http.Server{
		Addr:         *listenAddr,
		Handler:      proxy,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	trace.T("main", trace.PrioInfo, "listening at %q", *listenAddr)
	err = server.ListenAndServe()

	trace.T("main", trace.PrioInfo, "something went wrong: %s", err.Error())
}
