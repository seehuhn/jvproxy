package lib

import (
	"github.com/seehuhn/jvproxy/tester/test"
	"net/http"
	"time"
)

func HasCache(h test.Helper, _ ...interface{}) {
	h.SetInfo("", "7234")

	req := h.NewRequest("GET")
	header, _ := h.SendRequestToServer(req)

	lastModified := time.Now().Add(-25 * time.Hour)
	expires := time.Now().Add(50 * time.Hour)
	header.Set("Last-Modified", lastModified.Format(time.RFC1123))
	header.Set("Etag", "\"etag\"")
	header.Set("Expires", expires.Format(time.RFC1123))
	header.Set("Cache-Control", "public")

	h.SendResponseToClient(http.StatusOK, nil)

	req = h.NewRequest("GET")
	_, req = h.SendRequestToServer(req)
	if req == nil {
		h.Log("caching proxy detected")
	} else {
		h.Log("proxy seems not to be caching")
	}
}
