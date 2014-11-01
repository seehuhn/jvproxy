package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
)

type TestResult struct {
	Pass     bool
	Messages []string
}

type Test interface {
	Name() string
	Request() *http.Request
	Respond(w http.ResponseWriter, req *http.Request)
	Check(resp *http.Response, err error) *TestResult
}

type TestRunner struct {
	server *TestServer
	log    chan<- *LogEntry

	submit chan Test
	wait   chan struct{}

	transport *http.Transport
}

func NewTestRunner(server *TestServer, log chan<- *LogEntry) *TestRunner {
	res := &TestRunner{
		server: server,
		log:    log,

		submit: make(chan Test),
		wait:   make(chan struct{}),

		transport: &http.Transport{},
	}
	go res.processSubmissions()
	return res
}

func (run *TestRunner) Close() {
	close(run.submit)
	_ = <-run.wait
}

func (run *TestRunner) Run(t Test) {
	run.submit <- t
}

func (run *TestRunner) processSubmissions() {
	for t := range run.submit {
		entry := &LogEntry{}
		entry.Name = t.Name()

		req := t.Request()
		if req == nil {
			entry.Messages = append(entry.Messages,
				"failed to construct request")
			entry.TestFail = true
			run.log <- entry
		} else {
			req.URL.Scheme = "http"
			req.URL.Host = run.server.Addr

			run.server.Handler <- t.Respond

			resp, err := run.transport.RoundTrip(req)
			testResult := t.Check(resp, err)
			entry.ProxyFail = !testResult.Pass
			entry.Messages = append(entry.Messages, testResult.Messages...)
		}
		run.log <- entry
	}
	close(run.wait)
}

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

func (t SimpleTest) Check(resp *http.Response, err error) *TestResult {
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
