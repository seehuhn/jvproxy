package main

import (
	. "gopkg.in/check.v1"
	"io"
	"net/http"
)

type NullCache struct{}

func (cache *NullCache) Retrieve(*http.Request) *proxyResponse {
	return nil
}

func (cache *NullCache) StoreStart(string, int, http.Header) CacheEntry {
	return &nullEntry{}
}

func (cache *NullCache) Close() error {
	return nil
}

type nullEntry struct{}

func (entry *nullEntry) Reader(r io.Reader) io.Reader {
	return r
}
func (entry *nullEntry) Complete() {}
func (entry *nullEntry) Abort()    {}

func (s *MySuite) TestKeys(c *C) {
	testUrl := "http://example.com/test"
	key := urlToKey(testUrl, nil)
	url, fields, values := keyToUrl(key)
	c.Assert(url, Equals, testUrl)
	c.Assert(fields, HasLen, 0)
	c.Assert(values, HasLen, 0)

	h := http.Header{}
	h.Add("Vary", "B, A, C")
	h.Add("A", "first,  second")
	h.Add("A", "third")
	h.Add("C", "another")
	key = urlToKey(testUrl, h)
	url, fields, values = keyToUrl(key)
	c.Assert(url, Equals, testUrl)
	c.Assert(fields, DeepEquals, []string{"A", "B", "C"})
	c.Assert(values, DeepEquals, []string{"first, second, third", "", "another"})
}

var _ Cache = &NullCache{}
