package lib

import (
	"github.com/seehuhn/jvproxy/tester/test"
	"net/http"
	"time"
)

func HasValidate(h test.Helper, _ ...interface{}) {
	eTag := "\"" + test.RandomString(16) + "\""

	req := h.NewRequest("GET")
	header, _ := h.SendRequestToServer(req)

	now := time.Now()
	lastModified := now.Add(-25 * time.Hour)
	expires := now.Add(-1 * time.Minute)

	header.Set("Last-Modified", lastModified.Format(time.RFC1123))
	header.Set("Expires", expires.Format(time.RFC1123))
	header.Set("Etag", eTag)
	header.Set("Cache-Control", "public")

	h.SendResponseToClient(http.StatusOK, nil)

	req = h.NewRequest("GET")

	_, req = h.SendRequestToServer(req)

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
