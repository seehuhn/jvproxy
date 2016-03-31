package lib

import (
	"fmt"
	"net/http"

	"github.com/seehuhn/jvproxy/tester/test"
)

func Simple(h test.Helper, args ...interface{}) {
	n := args[0].(int)
	name := fmt.Sprintf("Simple-%d", n)
	h.SetInfo(name, "")
	req := h.NewRequest("GET")
	h.SendRequestToServer(req)
	body := &test.ResponseBodySpec{
		Seed:   0,
		Length: int64(n),
	}
	h.SendResponseToClient(http.StatusOK, body)
}
