package test

import (
	"net/http"
)

// The NoDate test exercises the requirements of RFC7231, section
// 7.1.1.2 (Date): "A recipient with a clock that receives a response
// message without a Date header field MUST record the time it was
// received and append a corresponding Date header field to the
// message's header section if it is cached or forwarded downstream."
type NoDate struct {
	msg string
}

func NewNoDate() *NoDate {
	return &NoDate{
		msg: UniqueString(128),
	}
}

func (t *NoDate) Info() *Info {
	return &Info{
		Name:   "NoDate",
		RFC:    "7231-7.1.1.2",
		Server: BreakDate,
	}
}

func (t *NoDate) Request() *http.Request {
	req, _ := http.NewRequest("GET", "/", nil)
	return req
}

func (t *NoDate) Respond(w http.ResponseWriter, req *http.Request) {
	w.Write([]byte(t.msg))
}

func (t *NoDate) Check(resp *http.Response, err error, up bool) *Result {
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

	if len(resp.Header["Date"]) == 0 {
		res.Pass = false
		res.Messages = append(res.Messages,
			"proxy failed to add missing date header")
	}

	resp.Body.Close()

	return res
}
