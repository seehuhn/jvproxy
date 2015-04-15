package test

import (
	"testing"
)

func specialTestTest(h Helper, args ...interface{}) {
	t := args[0].(*testing.T)
	req := h.NewRequest("GET", Special)
	_, req = h.ForwardRequest(req)

	resp := h.ForwardResponse()
	if _, ok := resp.Header["Date"]; ok {
		t.Error("special server response included a Date header")
	}
}

func TestSpecialServer(t *testing.T) {
	log := make(chan *LogEntry, 1)
	runner := NewRunner(nil, log)
	runner.Run(specialTestTest, t)
	res := <-log

	if len(res.Messages) < 1 || res.Messages[0] != specialServerMessage {
		t.Error("use of special server not mentioned in the log")
	}
}
