package main

import (
	"github.com/seehuhn/trace"
)

type LogEntry struct {
	RequestTimeNano int64
	RemoteAddr      string
	Method          string
	RequestURI      string

	StatusCode    int
	ContentLength int64
	Comments      []string

	ResponseReceivedNano int64
	HandlerCompleteNano  int64

	CacheResult string
}

func NewLogger() chan<- *LogEntry {
	res := make(chan *LogEntry, 64)
	go func() {
		for log := range res {
			trace.T("jvproxy/log", trace.PrioVerbose, "%v", log)
		}
	}()
	return res
}
