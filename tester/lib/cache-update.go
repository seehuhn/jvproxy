package lib

import (
	"github.com/seehuhn/jvproxy/tester/test"
	"net/http"
	"time"
)

func CacheUpdate(h test.Helper, _ ...interface{}) {
	eTag := "\"" + test.UniqueString(16) + "\""

	now := time.Now()
	lastModified := now.Add(-25 * time.Hour).Format(time.RFC1123)
	expires1 := now.Add(-2 * time.Minute).Format(time.RFC1123)
	expires2 := now.Add(-1 * time.Minute).Format(time.RFC1123)

	req := h.NewRequest("GET", test.Normal)
	header, _ := h.SendRequestToServer(req)
	header.Set("Cache-Control", "public")
	header.Set("Etag", eTag)
	header.Set("Expires", expires1)
	header.Set("Last-Modified", lastModified)
	header.Set("X-change", "old")
	header.Set("X-keep", "old")
	h.SendResponseToClient(http.StatusOK)

	req = h.NewRequest("GET", test.Normal)
	header, req = h.SendRequestToServer(req)
	if req == nil {
		h.Fail("proxy did not revalidate a cached response")
	}
	inm := req.Header.Get("If-None-Match")
	if inm == "" {
		h.Pass("proxy sent new upstream request (no caching?)")
	} else if inm != eTag {
		h.Fail("cache sent wrong ETag")
	}
	header.Set("Etag", eTag)
	header.Set("Expires", expires2)
	header.Set("X-change", "new")
	resp := h.SendResponseToClient(http.StatusNotModified)
	if resp.Header.Get("Expires") != expires2 {
		h.Fail("Expires header not updated")
	}
	if resp.Header.Get("X-keep") != "old" {
		h.Fail("X-keep header not kept")
	}
	if resp.Header.Get("X-change") != "new" {
		h.Fail("X-change header not updated")
	}
}