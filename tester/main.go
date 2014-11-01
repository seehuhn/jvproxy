package main

import (
	"flag"
	"net/url"

	"github.com/seehuhn/jvproxy/tester/lib"
	"github.com/seehuhn/jvproxy/tester/test"
	"net/http"
)

func main() {
	flag.Parse()

	log := lib.NewLogger()

	cacheIsShared := true
	proxy, err := url.Parse("http://localhost:4000")
	if err != nil {
		panic(err)
	}
	testRunner := lib.NewTestRunner(proxy, log.Submit)

	testRunner.Run(test.NewSimple("hello"))

	testRunner.Run(test.NewNoCache("rfc7234-3.0a", "XQRL", nil, nil, 200))
	testRunner.Run(test.NewNoCache("rfc7234-3.0b", "GET", nil, nil, 713))
	h := http.Header{}
	h.Add("Cache-Control", "no-store")
	testRunner.Run(test.NewNoCache("rfc7234-3.0c1", "GET", h, nil, 200))
	testRunner.Run(test.NewNoCache("rfc7234-3.0c2", "GET", nil, h, 200))
	if cacheIsShared {
		h = http.Header{}
		h.Add("Cache-Control", "private")
		testRunner.Run(test.NewNoCache("rfc7234-3.0d", "GET", nil, h, 200))

		h = http.Header{}
		h.Add("Authorization", "secret")
		testRunner.Run(test.NewNoCache("rfc7234-3.0e", "GET", h, nil, 200))
	}
	testRunner.Run(test.NewNoCache("rfc7234-3.0x", "GET", nil, nil, 200))

	log.Close()
}
