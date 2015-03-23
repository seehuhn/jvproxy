package test

import (
	"crypto/rand"
	"encoding/base64"
	"golang.org/x/crypto/sha3"
	"net/http"
	"sync"
)

type Test interface {
	Info() *Info
	Request() *http.Request
	Respond(w http.ResponseWriter, req *http.Request)
	Check(resp *http.Response, err error, serverCalled bool) *Result
}

type Breakage uint16

const (
	BreakDate Breakage = 1 << iota
)

// Info contains static information about a Test.
type Info struct {
	// Name is the name of the test for us in log output.
	Name string

	// RFC is the number and section of the RFC being tested.  For
	// example, the value "1111-2.3" would indicate that section 2.3
	// of RFC1111 is being tested.
	RFC string

	// Repeat indicates how often the test needs to be run.
	Repeat int

	// Server is a bit-field which indicates whether the test is meant
	// to be run with a HTTP server exhibiting certain peculiarities.
	Server Breakage
}

type CacheProperties uint16

const (
	IsCaching CacheProperties = 1 << iota
	DoesRevalidate
)

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

var testerSecret []byte

var seq int64 = 0
var seqLock sync.Mutex

func int64ToBytes(x int64) []byte {
	bytes := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		bytes[i] = byte(x & 0xff)
		x = x >> 8
	}
	return bytes
}

func UniqueString(length int) string {
	seqLock.Lock()
	k := seq
	seq += 1
	seqLock.Unlock()

	h := sha3.NewShake256()
	h.Write(testerSecret)
	h.Write(int64ToBytes(k))
	n := (length*6 + 7) / 8
	buf := make([]byte, n)
	h.Read(buf)

	res := base64.URLEncoding.EncodeToString(buf)
	return res[:length]
}

func init() {
	testerSecret = make([]byte, 32)
	_, err := rand.Read(testerSecret)
	if err != nil {
		panic("cannot generate random key: " + err.Error())
	}
}
