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
