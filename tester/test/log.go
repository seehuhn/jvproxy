package test

import (
	"time"
)

type LogEntry struct {
	Name     string
	TestFail bool

	Messages []string
	Features map[string]bool

	TotalTime, ReqTime, RespTime time.Duration
}

func (l *LogEntry) setTimes(times []timeStamps) {
	var cacheTime time.Duration
	var reqTime time.Duration
	var respTime time.Duration
	nCached := 0
	nForward := 0
	for _, t := range times {
		if t.ResponseSent.IsZero() {
			cacheTime += t.ResponseReceived.Sub(t.RequestSent)
			nCached++
		} else {
			reqTime += t.RequestReceived.Sub(t.RequestSent)
			respTime += t.ResponseReceived.Sub(t.ResponseSent)
			nForward++
		}
	}
	if nCached > 0 {
		l.TotalTime = cacheTime / time.Duration(nCached)
	}
	if nForward > 0 {
		l.ReqTime = reqTime / time.Duration(nForward)
		l.RespTime = respTime / time.Duration(nForward)
	}
}
