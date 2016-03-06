package main

import (
	"flag"
	"fmt"
	"github.com/seehuhn/jvproxy/tester/lib"
	"github.com/seehuhn/jvproxy/tester/test"
	"net/http"
	"net/url"
	"os"
	"strings"
)

var privateCacheFlag = flag.Bool("private", false,
	"test a private (per-user) cache")

func main() {
	flag.Parse()

	proxyURL := flag.Arg(0)
	if proxyURL == "" {
		proxyURL = "localhost:8080"
	}
	if !strings.Contains(proxyURL, "://") {
		proxyURL = "http://" + proxyURL
	}
	fmt.Println("testing proxy at", proxyURL)
	proxy, err := url.Parse(proxyURL)
	if err != nil {
		panic(err)
	}

	log := NewLogger()
	defer log.Close()
	testRunner := test.NewRunner(proxy, log.Submit)

	// test whether the proxy can be reached
	ok := testRunner.Run(lib.Simple, 64)
	if !ok {
		log.Close()
		fmt.Fprint(os.Stderr, "proxy failed, aborting ...\n")
		os.Exit(1)
	}

	// test requests of different sizes
	testRunner.Run(lib.Simple, 1024)
	testRunner.Run(lib.Simple, 1024*1024)

	// tests relating to caching
	testRunner.Run(lib.HasCache)

	var noHeaders http.Header
	testRunner.Run(lib.NoCache, "7234-3.0.a", "XQRL", noHeaders, noHeaders, 200)
	testRunner.Run(lib.NoCache, "7234-3.0.b", "GET", noHeaders, noHeaders, 713)
	h := http.Header{}
	h.Add("Cache-Control", "no-store")
	testRunner.Run(lib.NoCache, "7234-3.0.c-req", "GET", h, noHeaders, 200)
	testRunner.Run(lib.NoCache, "7234-3.0.c-resp", "GET", noHeaders, h, 200)
	if !*privateCacheFlag {
		h = http.Header{}
		h.Add("Cache-Control", "private")
		testRunner.Run(lib.NoCache, "7234-3.0.d", "GET", noHeaders, h, 200)

		h = http.Header{}
		h.Add("Authorization", "secret")
		testRunner.Run(lib.NoCache, "7234-3.0.e", "GET", h, noHeaders, 200)
	}
	// TODO(voss): codes 100, 101, 304?
	for _, code := range []int{201, 202, 205, 302, 303, 305, 307, 400, 401,
		402, 403, 406, 407, 408, 409, 411, 412, 413, 415, 416, 417, 426, 500,
		502, 503, 504, 505} {
		name := fmt.Sprintf("7234-3.0.f5-%d", code)
		testRunner.Run(lib.NoCache, name, "GET", noHeaders, noHeaders, code)
	}

	testRunner.Run(lib.AuthTest)

	h = http.Header{}
	h.Add("Cache-Control", "public")
	h.Add("Expires", "Thu, 01 Dec 1994 16:00:00 GMT")
	testRunner.Run(lib.NoCache, "7234-4.0f1", "GET", noHeaders, h, 200)

	// tests relating to validation of stale responses
	testRunner.Run(lib.HasValidate)
	testRunner.Run(lib.CacheUpdate)
}
