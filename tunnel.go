package jvproxy

import (
	"fmt"
	"io"
	"net"
	"sync"
)

func tunnel(server, client net.Conn) error {
	fmt.Println("tunnel started")
	defer fmt.Println("tunnel finished")

	children := &sync.WaitGroup{}

	children.Add(1)
	go func() {
		// buf := make([]byte, 4096)
		// var total int
		// var err error
		// for {
		//	var k, n int
		//	n, err = server.Read(buf)
		//	base := 0
		//	for base < n {
		//		k, err = client.Write(buf[base:n])
		//		base += k
		//		total += k
		//		if err != nil {
		//			break
		//		}
		//		err = client.Flush()
		//		if err != nil {
		//			break
		//		}
		//		fmt.Println("  .", k)
		//	}
		//	if err != nil {
		//		break
		//	}
		// }
		total, err := io.Copy(client, server)
		fmt.Printf("copied %d bytes through server->client tunnel: %v\n",
			total, err)
		client.Close()
		server.Close()
		children.Done()
	}()

	children.Add(1)
	go func() {
		n, err := io.Copy(server, client)
		fmt.Printf("copied %d bytes through client->server tunnel: %v\n",
			n, err)
		client.Close()
		server.Close()
		children.Done()
	}()

	children.Wait()

	return nil
}
