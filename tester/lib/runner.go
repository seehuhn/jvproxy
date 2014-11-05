package lib

import (
	"flag"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/seehuhn/jvproxy/tester/test"
)

var serveAddr = flag.String("addr", "localhost",
	"address to use for the test server")
var useIPv4 = flag.Bool("4", false, "use IPv4")
var useIPv6 = flag.Bool("6", false, "use IPv6")

type times struct {
	start, stop time.Time
}

type serverHint struct {
	path     string
	handler  http.HandlerFunc
	timeResp chan<- times
}

type TestRunner struct {
	log chan<- *LogEntry

	listener  net.Listener
	addr      string
	handler   chan *serverHint
	transport *http.Transport
}

func NewTestRunner(proxy *url.URL, log chan<- *LogEntry) *TestRunner {
	listener := getListener()
	transport := &http.Transport{
		Proxy: func(*http.Request) (*url.URL, error) { return proxy, nil },
	}
	res := &TestRunner{
		log: log,

		listener:  listener,
		addr:      listener.Addr().String(),
		handler:   make(chan *serverHint, 1),
		transport: transport,
	}
	go http.Serve(res.listener, http.HandlerFunc(res.serveHTTP))
	return res
}

func (run *TestRunner) Close() error {
	return run.listener.Close()
}

func (run *TestRunner) Run(t test.Test) {
	testInfo := t.Info()
	entry := &LogEntry{}
	entry.Name = testInfo.Name
	if testInfo.RFC != "" {
		entry.Name += " [RFC" + testInfo.RFC + "]"
	}
	for i := 0; i < testInfo.Repeat && !entry.TestFail && !entry.ProxyFail; i++ {
		run.doRun(t, entry)
	}
	run.log <- entry
}

func (run *TestRunner) doRun(t test.Test, entry *LogEntry) {
	req := t.Request()
	if req == nil {
		entry.Messages = append(entry.Messages,
			"failed to construct request")
		entry.TestFail = true
	} else {
		req.URL.Scheme = "http"
		req.URL.Host = run.addr

		timeResp := make(chan times, 1)
		run.handler <- &serverHint{
			path:     req.URL.Path,
			handler:  t.Respond,
			timeResp: timeResp,
		}

		sendTime := time.Now()
		resp, err := run.transport.RoundTrip(req)
		recvTime := time.Now()
		entry.TotalTime = recvTime.Sub(sendTime)

		serverCalled := true
		select {
		case _ = <-run.handler:
			serverCalled = false
		default:
			serverTimes := <-timeResp
			entry.ReqTime = serverTimes.start.Sub(sendTime)
			entry.RespTime = recvTime.Sub(serverTimes.stop)
		}
		testResult := t.Check(resp, err, serverCalled)
		entry.ProxyFail = !testResult.Pass
		entry.Messages = append(entry.Messages, testResult.Messages...)
	}
}

func (s *TestRunner) serveHTTP(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	hint := <-s.handler
	if hint.path != req.URL.Path {
		http.Error(w, "unexpected path "+req.URL.Path, http.StatusNotFound)
	} else {
		hint.handler(w, req)
	}
	stop := time.Now()
	hint.timeResp <- times{start, stop}
}

func getListener() (listener net.Listener) {
	tryIPv4 := true
	tryIPv6 := true
	if *useIPv4 {
		tryIPv6 = false
	} else if *useIPv6 {
		tryIPv4 = false
	}

	var err error
	if tryIPv6 {
		addr := "[" + *serveAddr + "]:0"
		listener, err = net.Listen("tcp6", addr)
		if err == nil {
			return listener
		}
	}
	if tryIPv4 {
		addr := *serveAddr + ":0"
		listener, err := net.Listen("tcp", addr)
		if err == nil {
			return listener
		}
	}
	panic("cannot listen on " + *serveAddr + ": " + err.Error())
}
