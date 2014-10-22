package main

import (
	"io"
	"io/ioutil"
	"net/http"
)

type Cache interface {
	Retrieve(*http.Request) *proxyResponse
	StoreStart(statusCode int, headers http.Header) CacheEntry
}

type CacheEntry interface {
	Body() io.Writer
	Abort()
	Complete()
}

type NullCache struct{}

func (cache *NullCache) Retrieve(*http.Request) *proxyResponse {
	return nil
}

func (cache *NullCache) StoreStart(int, http.Header) CacheEntry {
	return &nullEntry{}
}

type nullEntry struct{}

func (entry *nullEntry) Body() io.Writer {
	return ioutil.Discard
}
func (entry *nullEntry) Abort()    {}
func (entry *nullEntry) Complete() {}
