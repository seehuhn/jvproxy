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
			time.Sleep(10 * time.Millisecond)
			fmt.Fprintln(w, "Hello, client")
		}))
	defer upstream.Close()

	tempDir, err := ioutil.TempDir("", "testing")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	proxy := NewProxy("test", nil)
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
	viaProxy := &http.Client{Transport: transport}

	wait := &sync.WaitGroup{}
	maxParallel := 50
	tokens := make(chan bool, maxParallel)
	for i := 0; i < maxParallel; i++ {
		tokens <- true
	}
	for i := 0; i < 1000; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()

			token := <-tokens
			defer func() { tokens <- token }()

			resp, err := viaProxy.Get(upstream.URL)
			if err != nil {
				t.Error(err)
			}
			if resp == nil {
				return
			}
			if resp.StatusCode != 200 {
				t.Error("received status code", resp.Status)
			}
			_, err = ioutil.ReadAll(resp.Body)
			if err != nil && resp.StatusCode == 200 {
				t.Error(err)
			}
			err = resp.Body.Close()
			if err != nil && resp.StatusCode == 200 {
				t.Error(err)
			}
		}()
	}
	wait.Wait()
}
