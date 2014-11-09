package lib

import (
	"flag"
	"github.com/seehuhn/jvproxy/tester/test"
	"github.com/seehuhn/trace"
	"net"
	"net/http"
	"net/url"
	"time"
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

	listener net.Listener
	addr     string
	handler  chan *serverHint

	special     *specialServer
	specialAddr string

	transport *http.Transport
}

func NewTestRunner(proxy *url.URL, log chan<- *LogEntry) *TestRunner {
	listener := getListener()
	trace.T("jvproxy/tester/special", trace.PrioDebug,
		"ordinary server listening at %s", listener.Addr())
	special := newSpecialServer()
	transport := &http.Transport{
		Proxy: func(*http.Request) (*url.URL, error) { return proxy, nil },
	}
	res := &TestRunner{
		log: log,

		listener: listener,
		addr:     listener.Addr().String(),
		handler:  make(chan *serverHint, 1),

		special:     special,
		specialAddr: special.listener.Addr().String(),

		transport: transport,
	}
	go http.Serve(res.listener, http.HandlerFunc(res.serveHTTP))
	go res.special.Serve()
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
	path := "/" + test.UniqueString(32)
	for i := 0; i < testInfo.Repeat && !entry.TestFail && !entry.ProxyFail; i++ {
		run.doRun(t, path, entry)
	}
	run.log <- entry
}

func (run *TestRunner) doRun(t test.Test, path string, entry *LogEntry) {
	req := t.Request()
	if req == nil {
		entry.Messages = append(entry.Messages,
			"failed to construct request")
		entry.TestFail = true
	} else {
		req.URL.Scheme = "http"
		var handler chan *serverHint
		if _, ok := t.(test.SpecialTest); ok {
			entry.Messages = append(entry.Messages, "using special server")
			req.URL.Host = run.specialAddr
			handler = run.special.handler
		} else {
			req.URL.Host = run.addr
			handler = run.handler
		}
		req.URL.Path = path

		timeResp := make(chan times, 1)
		handler <- &serverHint{
			path:     path,
			handler:  t.Respond,
			timeResp: timeResp,
		}

		trace.T("jvproxy/tester/special", trace.PrioDebug,
			"requesting %s via proxy", req.URL)
		sendTime := time.Now()
		resp, err := run.transport.RoundTrip(req)
		recvTime := time.Now()
		if resp != nil {
			trace.T("jvproxy/tester/special", trace.PrioVerbose,
				"proxy response received: %v", resp)
		} else {
			trace.T("jvproxy/tester/special", trace.PrioDebug,
				"error while reading proxy response: %s", err)
		}
		entry.TotalTime = recvTime.Sub(sendTime)

		serverCalled := true
		select {
		case _ = <-handler:
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
