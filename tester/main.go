package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
)

type SimpleTest string

func (t SimpleTest) Name() string {
	return "SimpleTest (" + string(t) + ")"
}

func (t SimpleTest) Request() *http.Request {
	req, _ := http.NewRequest("GET", "/", nil)
	return req
}

func (t SimpleTest) Respond(w http.ResponseWriter, req *http.Request) {
	w.Write([]byte(t))
}

func (t SimpleTest) Check(resp *http.Response, err error, up bool) *TestResult {
	res := &TestResult{
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
	if string(data) != string(t) {
		res.Pass = false
		msg := fmt.Sprintf("wrong server response, expected %q, got %q",
			string(t), string(data))
		res.Messages = append(res.Messages, msg)
	}
	return res
}

func main() {
	flag.Parse()

	log := NewLogger()

	proxy, err := url.Parse("http://localhost:4000")
	if err != nil {
		panic(err)
	}
	testRunner := NewTestRunner(proxy, log.Submit)
	testRunner.Run(SimpleTest("hello"))
	testRunner.Run(SimpleTest("hello"))

	log.Close()
}
