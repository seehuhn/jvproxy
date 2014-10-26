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
	"flag"
	"github.com/seehuhn/trace"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var listenAddr = flag.String("listen-addr", "localhost:8080",
	"the address to listen on, in the form host:port")

var upstreamProxy = flag.String("upstream-proxy", "",
	"an upstream proxy to forward requests to")

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

	cache, err := NewLevelDBCache("cache")
	if err != nil {
		log.Fatalf("cannot create cache: %s", err.Error())
	}
	proxy := NewProxy(*listenAddr, transport, cache)

	server := &http.Server{
		Addr:         *listenAddr,
		Handler:      proxy,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	trace.T("main", trace.PrioInfo, "listening at %q", *listenAddr)
	server.ListenAndServe()
}
