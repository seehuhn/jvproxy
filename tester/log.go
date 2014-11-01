package main

import (
	"fmt"
)

type LogEntry struct {
	Name      string
	ProxyFail bool
	TestFail  bool
	Messages  []string
}

func NewLogger() chan<- *LogEntry {
	c := make(chan *LogEntry)
	go func() {
		for entry := range c {
			if entry.TestFail {
				fmt.Print("\n\n*** test failed ***\n")
				fmt.Println("TEST FAILURE", entry.Name)
			} else if entry.ProxyFail {
				fmt.Println("FAIL", entry.Name)
			} else {
				fmt.Println("PASS", entry.Name)
			}
			for _, msg := range entry.Messages {
				fmt.Println("  " + msg)
			}
		}
	}()
	return c
}
