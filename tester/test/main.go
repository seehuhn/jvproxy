// +build ignore

package main

import (
	"fmt"

	"github.com/seehuhn/jvproxy/tester/test"
)

func myTest(h test.Helper, args ...interface{}) {
	req := h.NewRequest("GET")
	header, req := h.SendRequestToServer(req)
	fmt.Println(header, req)
	task := &test.ResponseBodySpec{
		Seed:   1,
		Length: 100000000,
	}
	h.SendResponseToClient(200, task)
}

func main() {
	runner := test.NewRunner(nil, nil)
	runner.Run(myTest)
}
