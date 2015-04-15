package lib

import (
	"github.com/seehuhn/jvproxy/tester/test"
)

// The NoDate test exercises the requirements of RFC7231, section
// 7.1.1.2 (Date): "A recipient with a clock that receives a response
// message without a Date header field MUST record the time it was
// received and append a corresponding Date header field to the
// message's header section if it is cached or forwarded downstream."
func NoDate(h test.Helper, _ ...interface{}) {
	h.SetInfo("", "7231-7.1.1.2")
	req := h.NewRequest("GET", test.Special)
	h.ForwardRequest(req)
	resp := h.ForwardResponse()

	if len(resp.Header["Date"]) == 0 {
		h.Fail("proxy failed to add missing date header")
	}
}
