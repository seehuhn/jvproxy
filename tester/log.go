package main

import (
	"fmt"
	"github.com/seehuhn/jvproxy/tester/test"
	"time"
)

type Logger struct {
	Submit  chan<- *test.LogEntry
	receive <-chan *test.LogEntry
	done    chan struct{}
}

func NewLogger() *Logger {
	c := make(chan *test.LogEntry)
	res := &Logger{
		Submit:  c,
		receive: c,
		done:    make(chan struct{}),
	}
	go res.listen()
	return res
}

func (log *Logger) Close() {
	close(log.Submit)
	_ = <-log.done
}

func (log *Logger) listen() {
	for entry := range log.receive {
		if entry.TestFail {
			fmt.Print("\n\n*** test failed ***\n")
			fmt.Println("TEST FAILURE", entry.Name)
		} else {
			q := float64(time.Millisecond)
			fmt.Printf(".... %-32s %8.2fms %8.2fms %8.2fms\n", entry.Name,
				float64(entry.TotalTime)/q, float64(entry.ReqTime)/q,
				float64(entry.RespTime)/q)
		}
		for _, msg := range entry.Messages {
			fmt.Println("     * " + msg)
		}
	}
	close(log.done)
}
