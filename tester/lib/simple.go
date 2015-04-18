package lib

import (
	"github.com/seehuhn/jvproxy/tester/test"
)

func Simple(h test.Helper, _ ...interface{}) {
	req := h.NewRequest("GET", test.Normal)
	h.SendRequestToServer(req)
	h.SendResponseToClient()
}
