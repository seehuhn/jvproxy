package main

import (
	"fmt"
	"github.com/seehuhn/jvproxy/tester/test"
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
		if !entry.Pass {
			fmt.Print("\n\n*** test failed ***\n")
			fmt.Println("TEST FAILURE", entry.Name)
		} else {
			fmt.Printf(".... %-32s\n", entry.Name)
		}
		for _, msg := range entry.Messages {
			fmt.Println("     * " + msg)
		}
	}
	close(log.done)
}
