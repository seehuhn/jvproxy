package lib

import (
	"github.com/seehuhn/jvproxy/tester/test"
)

func Simple(h test.Helper, _ ...interface{}) {
	req := h.NewRequest("GET", test.Normal)
	h.ForwardRequest(req)
	h.ForwardResponse()
}
