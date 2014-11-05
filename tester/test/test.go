package test

import (
	"code.google.com/p/go.crypto/sha3"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sync"
)

type Test interface {
	Info() *Info
	Request() *http.Request
	Respond(w http.ResponseWriter, req *http.Request)
	Check(resp *http.Response, err error, serverCalled bool) *Result
}

type Info struct {
	Name   string
	RFC    string
	Repeat int
}

type Result struct {
	Pass     bool
	Messages []string
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

func UniquePath(length int) string {
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
