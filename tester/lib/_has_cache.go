package test

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"time"
)

// The HasCache test tries to establish whether the proxy is caching.
type HasCache struct {
}

func NewHasCache() *HasCache {
	return &HasCache{}
}

func (t *HasCache) Info() *Info {
	return &Info{
		Name:   "HasCache",
		RFC:    "7234",
		Repeat: 2,
	}
}

func (t *HasCache) Request(_ int) *http.Request {
	req, _ := http.NewRequest("GET", "/", nil)
	return req
}

func (t *HasCache) Respond(_ int, w http.ResponseWriter, req *http.Request) {
	lastModified := time.Now().Add(-25 * time.Hour)
	expires := time.Now().Add(50 * time.Hour)

	h := w.Header()
	h.Set("Last-Modified", lastModified.Format(time.RFC1123))
	h.Set("Etag", "\"etag\"")
	h.Set("Expires", expires.Format(time.RFC1123))
	h.Set("Cache-Control", "public")

	w.Write([]byte("hello"))
}

func (t *HasCache) Check(step int, resp *http.Response, err error, up bool) *Result {
	res := &Result{
		Pass: true,
	}

	if err != nil {
		res.Pass = false
		res.Messages = append(res.Messages,
			"error while reading response: "+err.Error())
	}
	if resp == nil {
		return res
	}

	bodyData, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		res.Pass = false
		res.Messages = append(res.Messages,
			"error while reading body: "+err.Error())
	}
	body := string(bodyData)

	if body != "hello" {
		res.Pass = false
		msg := fmt.Sprintf("wrong server response, expected %q, got %q",
			"hello", body)
		res.Messages = append(res.Messages, msg)
	} else if !up {
		res.Messages = append(res.Messages, "caching proxy detected")
		res.Detected |= IsCaching
	} else if up && step > 0 {
		res.Messages = append(res.Messages, "proxy seems not to be caching")
	}

	return res
}
