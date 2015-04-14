package test

import (
	"time"
)

type LogEntry struct {
	Name                         string
	TestFail                     bool
	Messages                     []string
	TotalTime, ReqTime, RespTime time.Duration
}
