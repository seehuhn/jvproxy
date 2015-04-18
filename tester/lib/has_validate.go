package lib

import (
	"github.com/seehuhn/jvproxy/tester/test"
	"time"
)

func HasValidate(h test.Helper, _ ...interface{}) {
	eTag := "\"" + test.UniqueString(16) + "\""

	req := h.NewRequest("GET", test.Normal)
	w, _ := h.ForwardRequest(req)

	now := time.Now()
	lastModified := now.Add(-25 * time.Hour)
	expires := now.Add(-1 * time.Minute)

	header := w.Header()
	header.Set("Last-Modified", lastModified.Format(time.RFC1123))
	header.Set("Expires", expires.Format(time.RFC1123))
	header.Set("Etag", eTag)
	header.Set("Cache-Control", "public")

	h.ForwardResponse()

	req = h.NewRequest("GET", test.Normal)

	_, req = h.ForwardRequest(req)

	if req == nil {
		h.Fail("proxy did not revalidate a cached response")
	}

	inm := req.Header.Get("If-None-Match")
	if inm == "" {
		h.Pass("proxy sent new upstream request (no caching?)")
	} else if inm != eTag {
		h.Fail("cache sent wrong ETag")
	}
	h.Log("successful validation detected")
}
