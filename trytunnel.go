// +build ignore

package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
)

func handleConnection(conn net.Conn) {
	fmt.Println("accepted", conn)
	defer conn.Close()
	status, err := bufio.NewReader(conn).ReadString('\n')
	fmt.Printf("received %q, %v\n", status, err)
	n, err := conn.Write([]byte("this is all good\n"))
	fmt.Printf("sent %d bytes, %v\n", n, err)
	fmt.Println("done", conn)
}

func server(ln net.Listener) {
	fmt.Println("server started")
	for {
		conn, err := ln.Accept()
		if err != nil {
			panic(err)
		}
		go handleConnection(conn)
	}
}

func main() {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		panic(err)
	}
	addr := ln.Addr().String()
	go server(ln)

	conn, err := net.Dial("tcp", "localhost:8080")
	if err != nil {
		panic(err)
	}
	msg := "CONNECT " + addr + " HTTP/1.1\r\nHost: " + addr + "\r\n\r\n"
	fmt.Fprintf(conn, msg)

	_, err = conn.Write([]byte("hello\n"))
	if err != nil {
		panic(err)
	}

	status, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil && err != io.EOF {
		panic(err)
	}
	fmt.Println(status)
}
