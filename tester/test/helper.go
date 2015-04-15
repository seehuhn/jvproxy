package test

import (
	"fmt"
	"github.com/seehuhn/trace"
	"io/ioutil"
	"net/http"
	"time"
)

type Helper interface {
	NewRequest(method string, tp serverType) *http.Request
	ForwardRequest(*http.Request) (http.ResponseWriter, *http.Request)
	ForwardResponse() *http.Response

	Log(format string, a ...interface{})
	Fail(format string, a ...interface{})
}

const specialServerMessage = "using special server"

type serverType int

const (
	Normal serverType = iota
	Special
)

type helper struct {
	runner *Runner
	log    *LogEntry
	path   string

	lastBody string

	lastRequestTime time.Time
	lastRequest     *requestFromProxy

	lastResponseTime  time.Time
	lastResponse      *http.Response
	lastResponseError error

	reqDurations        []time.Duration
	reqDurationsCount   int
	respDurations       []time.Duration
	respDurationsCount  int
	cacheDurations      []time.Duration
	cacheDurationsCount int

	waitForServer <-chan bool
}

func (h *helper) NewRequest(method string, tp serverType) *http.Request {
	var addr string
	switch tp {
	case Normal:
		addr = h.runner.normalAddr
	case Special:
		h.log.Messages = append(h.log.Messages, specialServerMessage)
		addr = h.runner.specialAddr
	default:
		panic("invalid server type")
	}
	req, err := http.NewRequest(method, "http://"+addr+h.path, nil)
	if err != nil {
		panic(err)
	}
	return req
}

func (h *helper) ForwardRequest(req *http.Request) (http.ResponseWriter, *http.Request) {
	if h.lastRequest != nil {
		panic(exMissingResponse)
	}

	waitForServer := make(chan bool, 1)
	go h.client(req, waitForServer)

	select {
	case s := <-h.runner.server:
		// The proxy contacted the server.
		h.waitForServer = waitForServer
		h.lastRequest = s
	case <-waitForServer:
		// The proxy did not contact the server.
		h.waitForServer = nil
		return nil, nil
	}

	req = h.lastRequest.req
	if req.URL.Path != h.path {
		panic(exWrongPath)
	}

	return h.lastRequest.w, req
}

func (h *helper) ForwardResponse() *http.Response {
	if h.waitForServer != nil {
		h.lastBody = UniqueString(64)
		h.lastRequest.w.Write([]byte(h.lastBody))
		close(h.lastRequest.done)

		<-h.waitForServer
		h.lastRequest = nil
		h.waitForServer = nil
	}

	err := h.lastResponseError
	if err != nil {
		h.log.TestFail = true
		h.log.Messages = append(h.log.Messages,
			"error while reading response: "+err.Error())
		return nil
	}

	resp := h.lastResponse
	if resp == nil {
		panic(exMissingRequest)
	}
	bodyData, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		h.log.TestFail = true
		h.log.Messages = append(h.log.Messages,
			"error while reading body: "+err.Error())
		return nil
	}
	body := string(bodyData)
	if body != h.lastBody {
		h.log.TestFail = true
		msg := fmt.Sprintf("wrong server response, expected %q, got %q",
			h.lastBody, body)
		h.log.Messages = append(h.log.Messages, msg)
		return nil
	}

	return resp
}

func (h *helper) Log(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	h.log.Messages = append(h.log.Messages, msg)
}

func (h *helper) Fail(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	panic(testFailure(msg))
}

func (h *helper) release() {
	if h.lastRequest != nil {
		close(h.lastRequest.done)
		panic(exMissingResponse)
	}
}

func (h *helper) client(req *http.Request, waitForServer chan<- bool) {
	trace.T("jvproxy/tester", trace.PrioDebug,
		"requesting %s via proxy", req.URL)
	h.lastRequestTime = time.Now()
	resp, err := h.runner.transport.RoundTrip(req)
	h.lastResponseTime = time.Now()
	if resp != nil {
		trace.T("jvproxy/tester", trace.PrioVerbose,
			"proxy response received: %s", resp.Status)
	} else {
		trace.T("jvproxy/tester", trace.PrioDebug,
			"error while reading proxy response: %s", err)
	}
	h.lastResponse = resp
	h.lastResponseError = err
	close(waitForServer)
}
