package test

import (
	"fmt"
	"math/rand"
	"net/http"
	"time"
)

type helperState int

const (
	stateReady helperState = iota
	stateReqSent
)

// Helper objects allow test cases to generate HTTP requests and
// responses, and to report the test outcome.  The only way to
// generate Helper objects is via the Runner.Run() method.
type Helper interface {
	NewRequest(method string) *http.Request
	SendRequestToServer(*http.Request) (http.Header, *http.Request)
	SendResponseToClient(int, *ResponseBodySpec) http.Header

	Log(format string, a ...interface{})
	Fail(format string, a ...interface{})
	Pass(format string, a ...interface{})

	SetInfo(name, RFC string)
}

type helper struct {
	state helperState

	path          string
	serverAddr    string
	transport     *http.Transport
	nextServerJob chan serverJob
	nextClientJob chan *clientJob

	serverCalled bool
	stats0       *clientStats
	stats1       *serverStats1
	stats2       *serverStats2

	log *LogEntry

	baseSeed int64
	nextSeed int64
}

func newHelper(addr string, transport *http.Transport, nextServerJob chan serverJob, log *LogEntry) *helper {
	path := "/" + log.Name + "/" + RandomString(16)
	h := &helper{
		state:         stateReady,
		path:          path,
		serverAddr:    addr,
		transport:     transport,
		nextServerJob: nextServerJob,
		nextClientJob: make(chan *clientJob, 1),
		log:           log,
		baseSeed:      time.Now().UnixNano(),
	}
	return h
}

func (h *helper) NewRequest(method string) *http.Request {
	req, err := http.NewRequest(method, "http://"+h.serverAddr+h.path, nil)
	if err != nil {
		panic(err)
	}
	return req
}

func (h *helper) SendRequestToServer(req *http.Request) (http.Header, *http.Request) {
	if h.state != stateReady {
		panic(exMissingResponse)
	}
	h.state = stateReqSent

	// tell the server what to expect.
	serverJob := make(chan *serverStats1)
	h.nextServerJob <- serverJob

	// send the request to the server
	go func() {
		countMutex.Lock()
		countClient++
		countMutex.Unlock()

		// The roundtrip to the server will only return after
		// .SendResponseToClient() has been called.
		timeA0 := time.Now()
		resp, err := h.transport.RoundTrip(req)
		timeB1 := time.Now()
		close(serverJob)

		if err != nil {
			panic(err)
		}

		respInfo := <-h.nextClientJob
		// TODO(voss): check that the status code is right.
		// TODO(voss): more checks here
		equal, err := checkBody(resp.Body, respInfo.Task)

		timeC1 := time.Now()
		resp.Body.Close()
		respInfo.StatsChan <- &clientStats{
			Header:      resp.Header,
			TimeA0:      timeA0,
			TimeB1:      timeB1,
			TimeC1:      timeC1,
			CorrectBody: equal,
			Err:         err,
		}
		close(respInfo.StatsChan)

		countMutex.Lock()
		countClient--
		if countClient < 0 {
			panic("client count corrupted")
		}
		countMutex.Unlock()
	}()

	// check whether the server was called
	stats1, serverCalled := <-serverJob
	h.serverCalled = serverCalled
	h.stats1 = stats1

	if !serverCalled {
		// Server did not collect serverJob, so we discard it here.
		tmp := <-h.nextServerJob
		if tmp != serverJob {
			panic("server out of sync")
		}
		return nil, nil
	}
	return stats1.Header, stats1.Req
}

type clientJob struct {
	Task      *ResponseBodySpec
	StatsChan chan<- *clientStats
}

type clientStats struct {
	Header      http.Header
	TimeA0      time.Time
	TimeB1      time.Time
	TimeC1      time.Time
	CorrectBody bool
	Err         error
}

type serverStats1 struct {
	TimeA1           time.Time
	Req              *http.Request
	Header           http.Header
	ResponseSpecChan chan<- *responseSpec
	Continuation     <-chan *serverStats2
}

type responseSpec struct {
	Status int
	*ResponseBodySpec
	Continuation chan<- *serverStats2
}

type serverStats2 struct {
	TimeB0 time.Time
	TimeC0 time.Time
	N      int64
	err    error
}

func (h *helper) SendResponseToClient(Status int, task *ResponseBodySpec) http.Header {
	if h.state != stateReqSent {
		panic(exMissingRequest)
	}
	h.state = stateReady

	// Use a per-helper seed offset, and allow for default tasks.
	var taskCopy ResponseBodySpec
	if task != nil {
		taskCopy = *task
	} else {
		h.nextSeed--
		taskCopy.Seed = h.nextSeed
		taskCopy.Length = 267 // any other number would also be ok
	}
	task = &taskCopy
	task.Seed += h.baseSeed

	// Trigger completion of the HTTP roundtrip.
	var stats2Chan chan *serverStats2
	if h.serverCalled {
		stats2Chan = make(chan *serverStats2)
		h.stats1.ResponseSpecChan <- &responseSpec{
			Status:           Status,
			ResponseBodySpec: task,
			Continuation:     stats2Chan,
		}
		close(h.stats1.ResponseSpecChan)
	}

	// Allow the client to verify the message body.
	clientStatsChan := make(chan *clientStats, 1)
	h.nextClientJob <- &clientJob{
		Task:      task,
		StatsChan: clientStatsChan,
	}

	// Collect the server-side stats about sending the message body.
	if h.serverCalled {
		h.stats2 = <-stats2Chan
	}

	h.stats0 = <-clientStatsChan

	return h.stats0.Header
}

func (h *helper) Log(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	h.log.Add(msg)
}

func (h *helper) Fail(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	panic(testFailure(msg))
}

func (h *helper) Pass(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	panic(testSuccess(msg))
}

func (h *helper) SetInfo(name, RFC string) {
	if name != "" {
		h.log.Name = name
	}
	if RFC != "" {
		h.log.Name += " [RFC" + RFC + "]"
	}
}

const validChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// RandomString returns a random string of length `n`, composed of
// upper- and lower-case letters as well as digits.
func RandomString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = validChars[rand.Intn(len(validChars))]
	}
	return string(b)
}
