package lib

import (
	"fmt"
	"time"
)

type LogEntry struct {
	Name                         string
	ProxyFail                    bool
	TestFail                     bool
	Messages                     []string
	TotalTime, ReqTime, RespTime time.Duration
}

type Logger struct {
	Submit  chan<- *LogEntry
	receive <-chan *LogEntry
	done    chan struct{}
}

func NewLogger() *Logger {
	c := make(chan *LogEntry)
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
		} else if entry.ProxyFail {
			fmt.Println("FAIL", entry.Name)
		} else {
			fmt.Println("....", entry.Name,
				entry.TotalTime, entry.ReqTime, entry.RespTime)
		}
		for _, msg := range entry.Messages {
			fmt.Println("  " + msg)
		}
	}
	close(log.done)
}
