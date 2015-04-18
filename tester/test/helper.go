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
	SendRequestToServer(*http.Request) (http.Header, *http.Request)
	SendResponseToClient(int) *http.Response

	Log(format string, a ...interface{})
	Fail(format string, a ...interface{})
	Pass(format string, a ...interface{})

	SetInfo(name, RFC string)
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

	lastRequest       *requestFromProxy
	lastResponse      *http.Response
	lastResponseError error

	times []timeStamps

	waitForServer <-chan bool
}

type timeStamps struct {
	RequestSent      time.Time
	RequestReceived  time.Time
	ResponseSent     time.Time
	ResponseReceived time.Time
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

func (h *helper) SendRequestToServer(req *http.Request) (http.Header, *http.Request) {
	if h.lastRequest != nil {
		panic(exMissingResponse)
	}
	h.times = append(h.times, timeStamps{})

	waitForServer := make(chan bool, 1)
	go h.client(req, waitForServer)

	select {
	case s := <-h.runner.server:
		// The proxy contacted the server.
		h.waitForServer = waitForServer
		h.lastRequest = s
		h.times[len(h.times)-1].RequestReceived = s.time
	case <-waitForServer:
		// The proxy did not contact the server.
		h.waitForServer = nil
		return nil, nil
	}

	req = h.lastRequest.req
	if req.URL.Path != h.path {
		panic(exWrongPath)
	}

	return h.lastRequest.w.Header(), req
}

func (h *helper) completeRequest(status int) {
	if h.lastRequest != nil {
		h.lastRequest.w.WriteHeader(status)
		if status == http.StatusOK {
			h.lastBody = UniqueString(64)
			h.times[len(h.times)-1].ResponseSent = time.Now()
			h.lastRequest.w.Write([]byte(h.lastBody))
		}

		close(h.lastRequest.done)

		<-h.waitForServer
		h.waitForServer = nil
		h.lastRequest = nil
	}
}

func (h *helper) SendResponseToClient(status int) *http.Response {
	h.completeRequest(status)

	err := h.lastResponseError
	if err != nil {
		msg := "error while reading response: " + err.Error()
		panic(testFailure(msg))
	}

	resp := h.lastResponse
	if resp == nil {
		panic(exMissingRequest)
	}
	bodyData, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		msg := "error while reading body: " + err.Error()
		panic(testFailure(msg))
	}
	body := string(bodyData)
	if body != h.lastBody {
		msg := fmt.Sprintf("wrong server response, expected %q, got %q",
			h.lastBody, body)
		panic(testFailure(msg))
	}

	return resp
}

func (h *helper) Log(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	h.log.Messages = append(h.log.Messages, msg)
}

func (h *helper) Pass(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	panic(testSuccess(msg))
}

func (h *helper) Fail(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	panic(testFailure(msg))
}

func (h *helper) SetInfo(name, RFC string) {
	if name != "" {
		h.log.Name = name
	}
	if RFC != "" {
		h.log.Name += " [RFC" + RFC + "]"
	}
}

func (h *helper) client(req *http.Request, waitForServer chan<- bool) {
	trace.T("jvproxy/tester", trace.PrioDebug,
		"requesting %s via proxy", req.URL)
	h.times[len(h.times)-1].RequestSent = time.Now()
	resp, err := h.runner.transport.RoundTrip(req)
	h.times[len(h.times)-1].ResponseReceived = time.Now()
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
