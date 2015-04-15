package test

import (
	"io/ioutil"
	"net/http"
	"strings"
	"time"
)

type Validate struct {
	eTag          string
	body          string
	didRevalidate bool
}

func NewValidate() *Validate {
	return &Validate{
		eTag: "\"match\"",
		body: UniqueString(64),
	}
}

func (t *Validate) Info() *Info {
	return &Info{
		Name:   "Validate",
		Repeat: 2,
	}
}

func (t *Validate) Request(int) *http.Request {
	req, _ := http.NewRequest("GET", "/", nil)
	return req
}

func (t *Validate) Respond(_ int, w http.ResponseWriter, req *http.Request) {
	now := time.Now()
	lastModified := now.Add(-1 * time.Hour)
	expires := now.Add(-1 * time.Second)

	h := w.Header()
	h.Set("Last-Modified", lastModified.Format(time.RFC1123))
	h.Set("Expires", expires.Format(time.RFC1123))
	h.Set("Etag", t.eTag)

	inm := req.Header.Get("If-None-Match")
	if inm != "" {
		t.didRevalidate = true
	}
	eMatch := inm == "*"
	if inm != "" && !eMatch {
		for _, word := range strings.Split(inm, ",") {
			word = strings.TrimSpace(word)
			if word == t.eTag {
				eMatch = true
				break
			}
		}
	}
	if eMatch {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Write([]byte(t.body))
}

func (t *Validate) Check(_ int, resp *http.Response, err error, up bool) *Result {
	res := &Result{
		Pass: true,
	}

	if err != nil {
		res.Pass = false
		res.Messages = append(res.Messages,
			"error while reading response: "+err.Error())
	}
	if resp == nil {
		return res
	}

	bodyData, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		res.Pass = false
		res.Messages = append(res.Messages,
			"error while reading body: "+err.Error())
	}
	body := string(bodyData)

	eTag := resp.Header.Get("Etag")
	if eTag != t.eTag {
		res.Pass = false
		res.Messages = append(res.Messages, "wrong Etag")
	} else if body != t.body {
		res.Pass = false
		res.Messages = append(res.Messages, "wrong Body")
	}
	if t.didRevalidate {
		res.Messages = append(res.Messages, "revalidation detected")
		res.Detected |= DoesRevalidate
	}

	return res
}
