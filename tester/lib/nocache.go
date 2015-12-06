package lib

import (
	"github.com/seehuhn/jvproxy/tester/test"
	"net/http"
)

// NoCache verifies that a server response is not cached by the proxy.
func NoCache(h test.Helper, args ...interface{}) {
	RFC := args[0].(string)
	method := args[1].(string)
	reqHeaders := args[2].(http.Header)
	respHeaders := args[3].(http.Header)
	statusCode := args[4].(int)

	h.SetInfo("", RFC)

	req := h.NewRequest(method, test.Normal)
	for key, vals := range reqHeaders {
		for _, val := range vals {
			req.Header.Add(key, val)
		}
	}
	header, _ := h.SendRequestToServer(req)
	for key, vals := range respHeaders {
		for _, val := range vals {
			header.Add(key, val)
		}
	}
	h.SendResponseToClient(statusCode)

	req = h.NewRequest(method, test.Normal)
	_, req = h.SendRequestToServer(req)
	if req == nil {
		h.Fail("proxy didn't contact server")
	}
}
