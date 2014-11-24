package test

import (
	"fmt"
	"io/ioutil"
	"net/http"
)

// Simple tests whether the proxy passes through simple GET requests
// to the server.
type Simple struct {
	msg string
}

func NewSimple() *Simple {
	return &Simple{
		msg: UniqueString(128),
	}
}

func (t *Simple) Info() *Info {
	return &Info{
		Name: "Simple",
	}
}

func (t *Simple) Request() *http.Request {
	req, _ := http.NewRequest("GET", "/", nil)
	return req
}

func (t *Simple) Respond(w http.ResponseWriter, req *http.Request) {
	w.Write([]byte(t.msg))
}

func (t *Simple) Check(resp *http.Response, err error, up bool) *Result {
	res := &Result{
		Pass: true,
	}

	if err != nil {
		res.Pass = false
		res.Messages = append(res.Messages,
			"error while reading headers: "+err.Error())
	}
	if resp == nil {
		return res
	}

	data, e2 := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err == nil && e2 != nil {
		res.Pass = false
		res.Messages = append(res.Messages,
			"error while reading body: "+e2.Error())
	}
	if string(data) != t.msg {
		res.Pass = false
		msg := fmt.Sprintf("wrong server response, expected %q, got %q",
			t.msg, string(data))
		res.Messages = append(res.Messages, msg)
	}
	return res
}
