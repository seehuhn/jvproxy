package lib

import (
	"net/http"

	"github.com/seehuhn/jvproxy/tester/test"
)

// AuthTest exercises the requirements of RFC7234, section 3.2
// (Storing Responses to Authenticated Requests).
func AuthTest(h test.Helper, _ ...interface{}) {
	h.SetInfo("", "7234-3.2")

	secret := test.RandomString(8)

	req := h.NewRequest("GET")
	req.Header.Add("Authorization", secret)
	_, req = h.SendRequestToServer(req)
	if req.Header.Get("Authorization") != secret {
		h.Fail("wrong/missing Authorization header")
	}
	h.SendResponseToClient(http.StatusOK, nil)

	req = h.NewRequest("GET")
	_, req = h.SendRequestToServer(req)
	if req == nil {
		h.Fail("proxy did not revalidate authenticated response")
	}
}
