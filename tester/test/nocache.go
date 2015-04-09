package test

import (
	"fmt"
	"io/ioutil"
	"net/http"
)

// The NoCache test verifies that a server response is not cached by
// the proxy.
type NoCache struct {
	rfc         string
	method      string
	reqHeaders  http.Header
	respHeaders http.Header
	statusCode  int

	body []string
}

func NewNoCache(RFC string, m string, h1, h2 http.Header, c int) *NoCache {
	return &NoCache{
		rfc:         RFC,
		method:      m,
		reqHeaders:  h1,
		respHeaders: h2,
		statusCode:  c,
	}
}

func (t *NoCache) Info() *Info {
	return &Info{
		Name:   "NoCache",
		RFC:    t.rfc,
		Repeat: 2,
	}
}

func (t *NoCache) Request(_ int) *http.Request {
	req, _ := http.NewRequest(t.method, "/", nil)
	for key, vals := range t.reqHeaders {
		for _, val := range vals {
			req.Header.Add(key, val)
		}
	}
	return req
}

func (t *NoCache) Respond(_ int, w http.ResponseWriter, req *http.Request) {
	body := UniqueString(64)
	t.body = append(t.body, body)

	h := w.Header()
	for key, vals := range t.respHeaders {
		for _, val := range vals {
			h.Add(key, val)
		}
	}
	w.WriteHeader(t.statusCode)
	w.Write([]byte(body))
}

func (t *NoCache) Check(step int, resp *http.Response, err error, up bool) *Result {
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

	if !up {
		res.Pass = false
		res.Messages = append(res.Messages, "proxy didn't contact server")
	} else if step > 1 && body == t.body[0] {
		res.Pass = false
		res.Messages = append(res.Messages, "body outdated response")
		res.Detected |= IsCaching
	} else if expected := t.body[len(t.body)-1]; body != expected {
		res.Pass = false
		msg := fmt.Sprintf("wrong server response, expected %q, got %q",
			expected, body)
		res.Messages = append(res.Messages, msg)
	}

	return res
}
