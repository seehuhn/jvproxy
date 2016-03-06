package test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func trivialTestTest(h Helper, _ ...interface{}) {
	req := h.NewRequest("GET")
	h.SendRequestToServer(req)
	h.SendResponseToClient(http.StatusOK, nil)
}

func TestRunner(t *testing.T) {
	log := make(chan *LogEntry, 1)
	runner := NewRunner(nil, log)
	runner.Run(trivialTestTest)
	res := <-log

	if len(res.Messages) > 0 {
		t.Errorf("trivial test produced messages: %v", res.Messages)
	}
	if !res.Pass {
		t.Error("trivial test failed")
	}

	err := runner.Close()
	if err != nil {
		t.Error(err)
	}
}

func missingResponseTestTest(h Helper, _ ...interface{}) {
	req := h.NewRequest("GET")
	h.SendRequestToServer(req)
	h.SendRequestToServer(req)
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
	h.SendResponseToClient(http.StatusOK, nil)
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

func incompleteTest1(h Helper, args ...interface{}) {
	req := h.NewRequest("GET")
	h.SendRequestToServer(req)
}

func incompleteTest2(h Helper, args ...interface{}) {
	req := h.NewRequest("GET")
	h.SendRequestToServer(req)
	h.SendResponseToClient(http.StatusOK, nil)
	h.SendRequestToServer(req)
}

func TestIncompleteTest(t *testing.T) {
	log := make(chan *LogEntry, 1)
	done := make(chan struct{})
	defer func() {
		close(log)
		_ = <-done
	}()
	go func() {
		for msg := range log {
			fmt.Println(msg)
		}
		close(done)
	}()

	runner := NewRunner(nil, log)

	success := runner.Run(trivialTestTest)
	if !success {
		t.Fatal("trivial test failed")
	}

	runner.Run(incompleteTest1)

	success = runner.Run(trivialTestTest)
	if !success {
		t.Fatal("trivial test failed")
	}

	runner.Run(missingRequestTestTest)

	success = runner.Run(trivialTestTest)
	if !success {
		t.Fatal("trivial test failed")
	}

	runner.Run(missingResponseTestTest)

	success = runner.Run(trivialTestTest)
	if !success {
		t.Fatal("trivial test failed")
	}

	runner.Run(incompleteTest2)

	success = runner.Run(trivialTestTest)
	if !success {
		t.Fatal("trivial test failed")
	}

	err := runner.Close()
	if err != nil {
		t.Error(err)
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

var blackHoleBody = &ResponseBodySpec{
	Seed:   1,
	Length: 8765,
}

func blackHoleProxyTestTest(h Helper, args ...interface{}) {
	t := args[0].(*testing.T)
	req := h.NewRequest("GET")
	_, req = h.SendRequestToServer(req)
	if req != nil {
		t.Fatal("server mysteriously contacted")
	}
	h.SendResponseToClient(http.StatusOK, blackHoleBody)

	// Try a second time, to make sure the nextServerJob channel
	// doesn't overflow and then block.
	req = h.NewRequest("GET")
	_, req = h.SendRequestToServer(req)
	if req != nil {
		t.Fatal("server mysteriously contacted")
	}
	h.SendResponseToClient(http.StatusOK, blackHoleBody)
}

func TestBlackHoleProxy(t *testing.T) {
	listener := getListener()
	go func() {
		// A "proxy" which magically knows the correct response
		// without asking the server.
		http.Serve(listener,
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				blackHoleBody.Write(w)
			}))
	}()
	defer listener.Close()
	proxy, err := url.Parse("http://" + listener.Addr().String())
	if err != nil {
		t.Fatal("cannot get proxy addr")
	}
	log := make(chan *LogEntry, 1)
	runner := NewRunner(proxy, log)
	pass := runner.Run(blackHoleProxyTestTest, t)
	msg := <-log
	if len(msg.Messages) > 0 {
		t.Errorf("black hole proxy test produced messages: %v", msg.Messages)
	}
	if !pass {
		t.Fatal("black hole proxy test failed")
	}
}
