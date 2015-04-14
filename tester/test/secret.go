package test

import (
	"crypto/rand"
	"encoding/base64"
	"golang.org/x/crypto/sha3"
	"sync"
)

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
	seq++
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
