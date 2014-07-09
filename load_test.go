package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"
)

func TestParallelAccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			fmt.Fprintln(w, "Hello, client")
		}))
	defer upstream.Close()

	tempDir, err := ioutil.TempDir("", "testing")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	proxy, err := NewProxy(tempDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	proxyServer := httptest.NewServer(proxy)
	defer proxyServer.Close()

	transport := &http.Transport{
		Proxy: func(r *http.Request) (*url.URL, error) {
			return url.Parse(proxyServer.URL)
		},
		DisableKeepAlives: true,
	}
	client := &http.Client{Transport: transport}

	wait := &sync.WaitGroup{}
	for i := 0; i < 10; i++ {
		i := i
		wait.Add(1)
		go func() {
			resp, err := client.Get(upstream.URL)
			if err != nil {
				t.Error(err)
			}
			_, err = ioutil.ReadAll(resp.Body)
			if err != nil {
				t.Error(err)
			}
			err = resp.Body.Close()
			if err != nil {
				t.Error(err)
			}
			fmt.Println(i)
			wait.Done()
		}()
	}
	wait.Wait()
}
