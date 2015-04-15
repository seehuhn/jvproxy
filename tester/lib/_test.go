package test

import (
	"net/http"
)

type Test interface {
	Info() *Info
	Request(step int) *http.Request
	Respond(step int, w http.ResponseWriter, req *http.Request)
	Check(step int, resp *http.Response, err error, serverCalled bool) *Result
}

// Result is used to record the outcome of a specific test case.
type Result struct {
	// Pass indicates whether the cache passed this specific test case.
	Pass bool

	// Messages is a slice of strings to include in the log file.
	Messages []string

	// Detected is a bit-field which records detected properties of
	// the cache being tested.
	Detected CacheProperties
}
