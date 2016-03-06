package test

import (
	"flag"
	"net"
	"net/http"
	"time"
)

var serveAddr = flag.String("addr", "localhost",
	"address to use for the test server")
var useIPv4 = flag.Bool("4", false, "use IPv4")
var useIPv6 = flag.Bool("6", false, "use IPv6")

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

type serverJob chan<- *serverStats1
type serverJobQueue <-chan serverJob

func (nextServerJob serverJobQueue) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	timeA1 := time.Now()

	countMutex.Lock()
	countServer++
	countMutex.Unlock()

	// the the job description
	serverJob := <-nextServerJob

	// TODO(voss): check that the URL is right

	// step 1: tell the client that the server was contacted.
	responseInfoChan := make(chan *responseSpec)
	serverJob <- &serverStats1{
		TimeA1:           timeA1,
		Req:              req,
		Header:           w.Header(),
		ResponseSpecChan: responseInfoChan,
	}
	// The serverJob channel is closed by the client once the
	// RoundTrip() call has completed.  This allows to determine if
	// the server was contacted or not (contacted, if the serverStats1
	// data arrives).

	// SendRequestToServer() returns (but client go routine still running) ...

	// SendResponseToClient() called ...

	// step 2: Wait until .SendResponseToClient() tells us which
	// status code and what response body to send.
	responseInfo := <-responseInfoChan

	// step 3: Write the response header.  This causes the
	// .Roundtrip() call in the client to return.
	timeB0 := time.Now()
	w.WriteHeader(responseInfo.Status)

	// step 4: Write the request body, as instructed by the
	// ResponseBodySpec structure.
	n, err := responseInfo.ResponseBodySpec.Write(w)
	timeC0 := time.Now()

	// step 5: Tell the client that the response has been written.
	responseInfo.Continuation <- &serverStats2{
		TimeB0: timeB0,
		TimeC0: timeC0,
		N:      n,
		err:    err,
	}
	close(responseInfo.Continuation)

	countMutex.Lock()
	countServer--
	if countServer < 0 {
		panic("server count corrupted")
	}
	countMutex.Unlock()
}
