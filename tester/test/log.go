package test

import (
	"strings"
)

// A LogEntry summarises the results of a single proxy test.  The log
// entry is composed of the proxy test name, a boolean to indicate
// whether the proxy passed the test, and a list of messages.
type LogEntry struct {
	Name     string
	Pass     bool
	Messages []string
}

// Add appends a new message to the log entry.
func (log *LogEntry) Add(msg string) {
	log.Messages = append(log.Messages, msg)
}

// String formats the log entry in a multi-line, human readable form.
func (log *LogEntry) String() string {
	var res []string
	status := "FAIL"
	if log.Pass {
		status = "OK"
	}
	res = append(res, log.Name+" "+status)
	for _, msg := range log.Messages {
		msg = strings.TrimSpace(msg)
		msg = strings.Replace(msg, "\n", "\n  ", -1)
		res = append(res, "- "+msg)
	}
	return strings.Join(res, "\n")
}
