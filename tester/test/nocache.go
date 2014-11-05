package test

import (
	"fmt"
	"io/ioutil"
	"net/http"
)

// Verify that a server response is not cached by the proxy.
type NoCache struct {
	RFC         string
	method      string
	reqHeaders  http.Header
	respHeaders http.Header
	statusCode  int

	path  string
	count int
	body  []string
}

func NewNoCache(RFC string, m string, h1, h2 http.Header, c int) *NoCache {
	return &NoCache{
		RFC:         RFC,
		method:      m,
		reqHeaders:  h1,
		respHeaders: h2,
		statusCode:  c,

		path: "/" + UniquePath(32),
	}
}

func (t *NoCache) Info() *Info {
	return &Info{
		Name:   "NoCache",
		RFC:    t.RFC,
		Repeat: 2,
	}
}

func (t *NoCache) Request() *http.Request {
	req, _ := http.NewRequest(t.method, t.path, nil)
	for key, vals := range t.reqHeaders {
		for _, val := range vals {
			req.Header.Add(key, val)
		}
	}
	return req
}

func (t *NoCache) Respond(w http.ResponseWriter, req *http.Request) {
	t.count += 1
	body := UniquePath(64)
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

func (t *NoCache) Check(resp *http.Response, err error, up bool) *Result {
	res := &Result{
		Pass: true,
	}

	if err != nil {
		res.Pass = false
		res.Messages = append(res.Messages,
			"error while reading headers: "+err.Error())
	}
	data, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		res.Pass = false
		res.Messages = append(res.Messages,
			"error while reading body: "+err.Error())
	}
	received := string(data)
	expected := t.body[len(t.body)-1]
	if t.count > 1 && received == t.body[0] {
		res.Pass = false
		res.Messages = append(res.Messages, "received outdated response")
	} else if received != expected {
		res.Pass = false
		msg := fmt.Sprintf("wrong server response, expected %q, got %q",
			expected, received)
		res.Messages = append(res.Messages, msg)
	} else if !up {
		res.Pass = false
		res.Messages = append(res.Messages, "proxy didn't contact server")
	}

	return res
}
