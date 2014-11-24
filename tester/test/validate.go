package test

import (
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Validate struct {
	lastModified time.Time
	expires      time.Time

	eTag  string
	extra string
	body  string

	count int
}

func NewValidate() *Validate {
	return &Validate{
		lastModified: time.Now().Add(-50 * time.Hour),

		eTag:  "\"match\"",
		extra: UniqueString(8),
		body:  UniqueString(64),
	}
}

func (t *Validate) Info() *Info {
	return &Info{
		Name:   "Validate",
		Repeat: 2,
	}
}

func (t *Validate) Request() *http.Request {
	t.count++
	req, _ := http.NewRequest("GET", "/", nil)
	return req
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func (t *Validate) Respond(w http.ResponseWriter, req *http.Request) {
	t.expires = time.Now().Add(-5 * time.Minute)

	h := w.Header()
	h.Set("Last-Modified", t.lastModified.Format(time.RFC1123))
	h.Set("Expires", t.expires.Format(time.RFC1123))
	h.Set("Etag", t.eTag)
	h.Set("X-Validate", strconv.Itoa(t.count))

	inm := req.Header.Get("If-None-Match")
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

	h.Set("X-Extra", t.extra)
	h.Set("X-Version", strconv.Itoa(t.count))
	w.Write([]byte(t.body))
}

func (t *Validate) Check(resp *http.Response, err error, up bool) *Result {
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
	if !up {
		res.Pass = false
		res.Messages = append(res.Messages, "proxy didn't contact server")
	} else if eTag != t.eTag {
		res.Pass = false
		res.Messages = append(res.Messages, "wrong Etag")
	} else if body != t.body {
		res.Pass = false
		res.Messages = append(res.Messages, "wrong Body")
	}

	return res
}
