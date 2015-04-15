package test

import (
	"github.com/seehuhn/trace"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"runtime"
	"time"
)

type requestFromProxy struct {
	time time.Time
	w    http.ResponseWriter
	req  *http.Request
	done chan<- bool
}

type Runner struct {
	log chan<- *LogEntry

	normalListener  net.Listener
	normalAddr      string
	specialListener net.Listener
	specialAddr     string
	transport       *http.Transport

	server chan *requestFromProxy
}

func NewRunner(proxy *url.URL, log chan<- *LogEntry) *Runner {
	normalListener := getListener()
	specialListener := getListener()

	transport := &http.Transport{}
	if proxy != nil {
		transport.Proxy =
			func(*http.Request) (*url.URL, error) { return proxy, nil }
	}

	run := &Runner{
		log: log,

		normalListener:  normalListener,
		normalAddr:      normalListener.Addr().String(),
		specialListener: specialListener,
		specialAddr:     specialListener.Addr().String(),
		transport:       transport,

		server: make(chan *requestFromProxy, 1),
	}
	go http.Serve(run.normalListener, http.HandlerFunc(run.serveHTTP))
	trace.T("jvproxy/tester", trace.PrioDebug,
		"normal server listening at %s", run.normalAddr)
	go serveSpecial(run.specialListener, http.HandlerFunc(run.serveHTTP))
	trace.T("jvproxy/tester", trace.PrioDebug,
		"special server listening at %s", run.specialAddr)
	return run
}

func (run *Runner) Close() error {
	return run.normalListener.Close()
}

func (run *Runner) Run(test Case, args ...interface{}) {
	log := &LogEntry{}
	fptr := reflect.ValueOf(test).Pointer()
	log.Name = runtime.FuncForPC(fptr).Name()
	defer func() {
		if r := recover(); r != nil {
			if msg, ok := r.(brokenTest); ok {
				log.Messages = append(log.Messages,
					"BROKEN TEST: "+string(msg))
			} else if msg, ok := r.(testFailure); ok {
				log.Messages = append(log.Messages, string(msg))
			} else {
				panic(r)
			}
			log.TestFail = true
		}
		run.log <- log
	}()

	proxy := &helper{
		runner: run,
		log:    log,
		path:   "/" + log.Name + "/" + UniqueString(16),
	}
	defer proxy.release()

	test(proxy, args...)
}

func (run *Runner) serveHTTP(w http.ResponseWriter, req *http.Request) {
	done := make(chan bool)
	run.server <- &requestFromProxy{
		time: time.Now(),
		w:    w,
		req:  req,
		done: done,
	}
	<-done
}
