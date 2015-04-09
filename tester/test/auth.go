package test

import (
	"fmt"
	"io/ioutil"
	"net/http"
)

// AuthTest exercises the requirements of RFC7234, section 3.2
// (Storing Responses to Authenticated Requests).
type AuthTest struct{}

func NewAuthTest() *AuthTest {
	return &AuthTest{}
}

func (t *AuthTest) Info() *Info {
	return &Info{
		Name:   "Auth",
		RFC:    "7234-3.2",
		Repeat: 2,
	}
}

func (t *AuthTest) Request(step int) *http.Request {
	req, _ := http.NewRequest("GET", "/", nil)
	if step == 0 {
		req.Header.Add("Authorization", "secret")
	}
	return req
}

func (t *AuthTest) Respond(_ int, w http.ResponseWriter, req *http.Request) {
	if req.Header.Get("Authorization") == "secret" {
		w.Write([]byte("allowed"))
	} else {
		w.Write([]byte("denied"))
	}
}

func (t *AuthTest) Check(step int, resp *http.Response, err error, up bool) *Result {
	res := &Result{
		Pass: true,
	}

	if err != nil {
		res.Pass = false
		res.Messages = append(res.Messages,
			"error while reading headers: "+err.Error())
	}
	if resp == nil {
		return res
	}

	data, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		res.Pass = false
		res.Messages = append(res.Messages,
			"error while reading body: "+err.Error())
	}
	received := string(data)

	if step > 0 {
		expected := "denied"
		if !up {
			res.Pass = false
			res.Messages = append(res.Messages, "proxy didn't contact server")
		} else if received != expected {
			res.Pass = false
			msg := fmt.Sprintf("wrong server response, expected %q, got %q",
				expected, received)
			res.Messages = append(res.Messages, msg)
		}
	} else {
		expected := "allowed"
		if !up {
			res.Pass = false
			res.Messages = append(res.Messages, "proxy didn't contact server")
		} else if received != expected {
			res.Pass = false
			msg := fmt.Sprintf("wrong server response, expected %q, got %q",
				expected, received)
			res.Messages = append(res.Messages, msg)
		}
	}
	return res
}
