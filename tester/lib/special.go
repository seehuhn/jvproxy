package lib

import (
	"bufio"
	"fmt"
	"github.com/seehuhn/trace"
	"io"
	"net"
	"net/http"
	"time"
)

type specialServer struct {
	listener net.Listener
	handler  chan *serverHint
}

func newSpecialServer() *specialServer {
	res := &specialServer{
		listener: getListener(),
		handler:  make(chan *serverHint, 1),
	}
	trace.T("jvproxy/tester/special", trace.PrioDebug,
		"special server listening at %s", res.listener.Addr())
	return res
}

func (s *specialServer) Serve() {
	var tempDelay time.Duration // how long to sleep on accept failure
	for {
		c, err := s.listener.Accept()
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
		start := time.Now()

		hint := <-s.handler
		trace.T("jvproxy/tester/special", trace.PrioVerbose,
			"serving path %s only", hint.path)

		if req == nil {
			trace.T("jvproxy/tester/special", trace.PrioDebug,
				"invalid request from %s: %s", c.RemoteAddr(), err)
		} else {
			trace.T("jvproxy/tester/special", trace.PrioDebug,
				"received request from %s for %s",
				c.RemoteAddr(), req.URL)

			if hint.path != req.URL.Path {
				http.Error(w, "unexpected path "+req.URL.Path, http.StatusNotFound)
			} else {
				hint.handler(w, req)
			}
		}

		stop := time.Now()
		c.Close()
		hint.timeResp <- times{start, stop}
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
