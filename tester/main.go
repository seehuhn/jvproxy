package main

import (
	"flag"
	"fmt"
	"net/http"
)

func answer(w http.ResponseWriter, req *http.Request) {
	w.Write([]byte("hello\n"))
}

func main() {
	flag.Parse()

	testServer := startServer()
	fmt.Println("server address:", testServer.Addr)

	log := NewLogger()

	testRunner := NewTestRunner(testServer, log)
	testRunner.Run(SimpleTest("hello"))
	testRunner.Close()

	close(log)
}
