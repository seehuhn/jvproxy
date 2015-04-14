package test

import (
	"bufio"
	"flag"
	"fmt"
	"github.com/seehuhn/trace"
	"io"
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

func serveSpecial(listener net.Listener, handler http.Handler) {
	var tempDelay time.Duration // how long to sleep on accept failure
	for {
		c, err := listener.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if max := 1 * time.Second; tempDelay > max {
					tempDelay = max
				}
				time.Sleep(tempDelay)
				continue
			}
			break
		}
		tempDelay = 0

		w := &specialResponseWriter{
			Conn:   c,
			header: http.Header{},
		}
		w.header.Add("Connection", "close")

		trace.T("jvproxy/tester/special", trace.PrioVerbose,
			"accepted connection from %s", c.RemoteAddr())
		req, err := http.ReadRequest(bufio.NewReader(c))

		if req == nil {
			trace.T("jvproxy/tester/special", trace.PrioDebug,
				"invalid request from %s: %s", c.RemoteAddr(), err)
		} else {
			trace.T("jvproxy/tester/special", trace.PrioDebug,
				"received request from %s for %s",
				c.RemoteAddr(), req.URL)
			handler.ServeHTTP(w, req)
		}
		c.Close()
	}
}

type specialResponseWriter struct {
	Conn        io.Writer
	header      http.Header
	WroteHeader bool
}

func (srw *specialResponseWriter) Header() http.Header {
	return srw.header
}

func (srw *specialResponseWriter) WriteHeader(code int) {
	if srw.WroteHeader {
		panic("header written twice")
	}
	fmt.Fprintf(srw.Conn, "HTTP/1.1 %d %s\r\n", code, http.StatusText(code))
	srw.header.Write(srw.Conn)
	srw.Conn.Write([]byte("\r\n"))
	srw.WroteHeader = true
}

func (srw *specialResponseWriter) Write(data []byte) (int, error) {
	if !srw.WroteHeader {
		srw.WriteHeader(http.StatusOK)
	}
	return srw.Conn.Write(data)
}
