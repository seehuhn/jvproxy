package jvproxy

import (
	"fmt"
	"os"
	"time"
)

type LogEntry struct {
	RequestTime time.Time
	RemoteAddr  string
	Method      string
	RequestURI  string

	StatusCode    int
	ContentLength int64
	Comments      []string

	ResponseReceivedNano int64
	HandlerCompleteNano  int64

	CacheResult string
}

var logChannel chan *LogEntry

func logger() {
	outFile, err := os.OpenFile("access.log",
		os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		panic(err)
	}
	for log := range logChannel {
		t := log.RequestTime.Format("2006-01-02 15:04:05.999")
		_, err = fmt.Fprintf(outFile, "%-23s %-16s %-4s %s\n"+
			"                        %d %d %s %s\n",
			t, log.RemoteAddr, log.Method, log.RequestURI,
			log.StatusCode, log.ContentLength, log.CacheResult, log.Comments)
		if err != nil {
			panic(err)
		}
		outFile.Sync() // TODO(voss): remove?
	}
}

func NewLogger() chan<- *LogEntry {
	return logChannel
}

func init() {
	logChannel = make(chan *LogEntry, 64)
	go logger()
}
