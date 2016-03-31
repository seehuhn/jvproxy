package lib

import (
	"net/http"
	"time"

	"github.com/seehuhn/jvproxy/tester/test"
)

func CacheUpdate(h test.Helper, _ ...interface{}) {
	eTag := "\"" + test.RandomString(16) + "\""

	now := time.Now()
	lastModified := now.Add(-25 * time.Hour).Format(time.RFC1123)
	expires1 := now.Add(-2 * time.Minute).Format(time.RFC1123)
	expires2 := now.Add(-1 * time.Minute).Format(time.RFC1123)

	req := h.NewRequest("GET")
	header, _ := h.SendRequestToServer(req)
	header.Set("Cache-Control", "public")
	header.Set("Etag", eTag)
	header.Set("Expires", expires1)
	header.Set("Last-Modified", lastModified)
	header.Set("X-change", "old")
	header.Set("X-keep", "old")
	body := &test.ResponseBodySpec{
		Seed:   1,
		Length: 1000,
	}
	h.SendResponseToClient(http.StatusOK, body)

	req = h.NewRequest("GET")
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
	header = h.SendResponseToClient(http.StatusNotModified, body)
	if header.Get("Expires") != expires2 {
		h.Fail("Expires header not updated")
	}
	if header.Get("X-keep") != "old" {
		h.Fail("X-keep header not kept")
	}
	if header.Get("X-change") != "new" {
		h.Fail("X-change header not updated")
	}
}
