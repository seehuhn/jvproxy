package test

import (
	"strings"
	"testing"
)

func trivialTestTest(h Helper, _ ...interface{}) {
	req := h.NewRequest("GET", Normal)
	h.ForwardRequest(req)
	h.ForwardResponse()
}

func TestRunner(t *testing.T) {
	log := make(chan *LogEntry, 1)
	runner := NewRunner(nil, log)
	runner.Run(trivialTestTest)
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

func TestMissingResponseTest(t *testing.T) {
	log := make(chan *LogEntry, 1)
	runner := NewRunner(nil, log)
	runner.Run(missingResponseTestTest)
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

func TestMissingRequestTest(t *testing.T) {
	log := make(chan *LogEntry, 1)
	runner := NewRunner(nil, log)
	runner.Run(missingRequestTestTest)
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

func failTestTest(h Helper, args ...interface{}) {
	t := args[0].(*testing.T)
	msg := args[1].(string)
	h.Fail(msg)
	t.Error("Helper.Fail() did not abort test case")
}

func TestFailTest(t *testing.T) {
	msg := "test fail message"
	log := make(chan *LogEntry, 1)
	runner := NewRunner(nil, log)
	runner.Run(failTestTest, t, msg)
	entry := <-log
	if len(entry.Messages) != 1 || entry.Messages[0] != msg {
		t.Error("missing test failure message")
	}
}
