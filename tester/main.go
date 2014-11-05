package main

import (
	"flag"
	"fmt"
	"github.com/seehuhn/jvproxy/tester/lib"
	"github.com/seehuhn/jvproxy/tester/test"
	"net/http"
	"net/url"
	"strings"
)

func main() {
	flag.Parse()

	proxyUrl := flag.Arg(0)
	if proxyUrl == "" {
		proxyUrl = "localhost:8080"
	}
	if !strings.Contains(proxyUrl, "://") {
		proxyUrl = "http://" + proxyUrl
	}
	fmt.Println("testing proxy at", proxyUrl)

	log := lib.NewLogger()

	cacheIsShared := true
	proxy, err := url.Parse(proxyUrl)
	if err != nil {
		panic(err)
	}
	testRunner := lib.NewTestRunner(proxy, log.Submit)

	testRunner.Run(test.NewSimple())
	testRunner.Run(test.NewNoCache("7234-3.0.a", "XQRL", nil, nil, 200))
	testRunner.Run(test.NewNoCache("7234-3.0.b", "GET", nil, nil, 713))
	h := http.Header{}
	h.Add("Cache-Control", "no-store")
	testRunner.Run(test.NewNoCache("7234-3.0.c-req", "GET", h, nil, 200))
	testRunner.Run(test.NewNoCache("7234-3.0.c-resp", "GET", nil, h, 200))
	if cacheIsShared {
		h = http.Header{}
		h.Add("Cache-Control", "private")
		testRunner.Run(test.NewNoCache("7234-3.0.d", "GET", nil, h, 200))

		h = http.Header{}
		h.Add("Authorization", "secret")
		testRunner.Run(test.NewNoCache("7234-3.0.e", "GET", h, nil, 200))
	}
	// TODO(voss): codes 100, 101, 304?
	for _, code := range []int{201, 202, 205, 302, 303, 305, 307, 400, 401,
		402, 403, 406, 407, 408, 409, 411, 412, 413, 415, 416, 417, 426, 500,
		502, 503, 504, 505} {
		name := fmt.Sprintf("7234-3.0.f5-%d", code)
		testRunner.Run(test.NewNoCache(name, "GET", nil, nil, code))
	}

	log.Close()
}
