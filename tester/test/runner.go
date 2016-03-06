package test

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"runtime"
	"strings"
	"sync"
)

const testPrefix = "github.com/seehuhn/jvproxy/tester/lib."

var countMutex sync.Mutex
var countClient int
var countServer int

// A Runner provides the infrastructure to run a series of tests on a
// HTTP proxy.
type Runner struct {
	log chan<- *LogEntry

	listener   net.Listener
	serverAddr string
	transport  *http.Transport

	nextServerJob chan serverJob
}

// NewRunner allocates a new test.Runner for testing the given proxy.
// Test results will be written to the `LogEntry` channel.
func NewRunner(proxy *url.URL, log chan<- *LogEntry) *Runner {
	listener := getListener()
	transport := &http.Transport{}
	if proxy != nil {
		transport.Proxy =
			func(*http.Request) (*url.URL, error) { return proxy, nil }
	}

	nextServerJob := make(chan serverJob, 1)

	go func() {
		http.Serve(listener, serverJobQueue(nextServerJob))
	}()

	return &Runner{
		log: log,

		listener:   listener,
		serverAddr: listener.Addr().String(),
		transport:  transport,

		nextServerJob: nextServerJob,
	}
}

// Close shuts down the test runner and frees all resources associated
// to the test runner.
func (run *Runner) Close() error {
	// This stops the HTTP server started by NewRunner().
	return run.listener.Close()
}

// Run executes a single test case.  The optional arguments `args` are
// passed through to the test case.
func (run *Runner) Run(test Case, args ...interface{}) (pass bool) {
	log := &LogEntry{
		Pass: true,
	}
	fptr := reflect.ValueOf(test).Pointer()
	log.Name = runtime.FuncForPC(fptr).Name()
	if strings.HasPrefix(log.Name, testPrefix) {
		log.Name = log.Name[len(testPrefix):]
	}

	helper := newHelper(run.serverAddr, run.transport, run.nextServerJob, log)

	defer func() {
		if helper.state != stateReady {
			task := &ResponseBodySpec{
				Length: 0,
			}
			helper.SendResponseToClient(http.StatusInternalServerError, task)
		}

		countMutex.Lock()
		if countClient != 0 {
			msg := fmt.Sprintf("client count %d != 0", countClient)
			log.Add(msg)
		}
		if countServer != 0 {
			msg := fmt.Sprintf("server count %d != 0", countServer)
			log.Add(msg)
		}
		countMutex.Unlock()

		if r := recover(); r != nil {
			if msg, ok := r.(brokenTest); ok {
				log.Pass = false
				log.Add("BROKEN TEST: " + string(msg))
			} else if msg, ok := r.(testFailure); ok {
				log.Pass = false
				log.Add(string(msg))
			} else if msg, ok := r.(testSuccess); ok {
				log.Pass = true
				log.Add(string(msg))
			} else {
				panic(r)
			}
		}
		if run.log != nil {
			run.log <- log
		}

		pass = log.Pass
	}()

	test(helper, args...)

	return
}
