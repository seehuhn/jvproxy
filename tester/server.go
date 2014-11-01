package main

import (
	"flag"
	"net"
	"net/http"
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

type TestServer struct {
	listener    net.Listener
	Addr        string
	Handler     chan<- http.HandlerFunc
	handlerChan <-chan http.HandlerFunc
}

func (s *TestServer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	handler, ok := <-s.handlerChan
	if ok {
		handler(w, req)
	} else {
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}
}

func startServer() *TestServer {
	c := make(chan http.HandlerFunc, 1)
	listener := getListener()
	testServer := &TestServer{
		listener:    listener,
		Addr:        listener.Addr().String(),
		Handler:     c,
		handlerChan: c,
	}

	s := http.Server{
		Handler: testServer,
	}
	go s.Serve(listener)

	return testServer
}
