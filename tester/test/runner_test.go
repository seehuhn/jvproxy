package test

import (
	"strings"
	"testing"
)

func trivialTestTest(h Helper, _ ...interface{}) {
	req := h.NewRequest("GET", Normal)
	_, req = h.ForwardRequest(req)
	h.ForwardResponse()
}

func TestRunner(t *testing.T) {
	log := make(chan *LogEntry, 1)
	runner := NewRunner(nil, log)
	runner.Run("trivalTestTest", trivialTestTest)
	res := <-log

	if len(res.Messages) > 0 {
		t.Errorf("trivial test produced messages: %v", res.Messages)
	}
	if res.TestFail {
		t.Error("trivial test failed")
	}

	err := runner.Close()
	if err != nil {
		t.Error(err)
	}
}

func missingResponseTestTest(h Helper, _ ...interface{}) {
	req := h.NewRequest("GET", Normal)
	h.ForwardRequest(req)
	h.ForwardRequest(req)
}

func TestMissingResponse(t *testing.T) {
	log := make(chan *LogEntry, 1)
	runner := NewRunner(nil, log)
	runner.Run("missingResponseTestTest", missingResponseTestTest)
	entry := <-log
	if !strings.HasSuffix(entry.Messages[0], string(exMissingResponse)) {
		t.Fatalf("missing response not detected: %s",
			entry.Messages[0])
	}

	err := runner.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func missingRequestTestTest(h Helper, _ ...interface{}) {
	h.ForwardResponse()
}

func TestMissingRequest(t *testing.T) {
	log := make(chan *LogEntry, 1)
	runner := NewRunner(nil, log)
	runner.Run("missingRequestTestTest", missingRequestTestTest)
	entry := <-log
	if !strings.HasSuffix(entry.Messages[0], string(exMissingRequest)) {
		t.Fatalf("missing request not detected: %s",
			entry.Messages[0])
	}

	err := runner.Close()
	if err != nil {
		t.Fatal(err)
	}
}
