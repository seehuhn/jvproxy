package test

import (
	"fmt"
	"io/ioutil"
	"net/http"
)

type Simple struct {
	msg  string
	path string
}

func NewSimple(msg string) *Simple {
	return &Simple{
		msg:  msg,
		path: UniquePath(32),
	}
}

func (t *Simple) Name() string {
	return fmt.Sprintf("Simple (%s)", t.msg)
}

func (t *Simple) Request() *http.Request {
	req, _ := http.NewRequest("GET", "/"+t.path, nil)
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
	data, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		res.Pass = false
		res.Messages = append(res.Messages,
			"error while reading body: "+err.Error())
	}
	if string(data) != t.msg {
		res.Pass = false
		msg := fmt.Sprintf("wrong server response, expected %q, got %q",
			t.msg, string(data))
		res.Messages = append(res.Messages, msg)
	}
	return res
}
