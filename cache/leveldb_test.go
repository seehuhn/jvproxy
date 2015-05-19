package cache

import (
	. "gopkg.in/check.v1"
	"net/http"
	"time"
)

func (s *MySuite) TestKeys(c *C) {
	testURL := "http://example.com/test"
	key := urlToKey(testURL, nil)
	url, fields, values := keyToURL(key)
	c.Assert(url, Equals, testURL)
	c.Assert(fields, HasLen, 0)
	c.Assert(values, HasLen, 0)

	h := http.Header{}
	h.Add("Vary", "B, A, C")
	h.Add("A", "first,  second")
	h.Add("A", "third")
	h.Add("C", "another")
	key = urlToKey(testURL, h)
	url, fields, values = keyToURL(key)
	c.Assert(url, Equals, testURL)
	c.Assert(fields, DeepEquals, []string{"A", "B", "C"})
	c.Assert(values, DeepEquals, []string{"first,second,third", "", "another"})
}

func (s *MySuite) TestMetaDataEncoding(c *C) {
	meta := &MetaData{
		StatusCode:    200,
		Header:        make(http.Header),
		ResponseTime:  time.Now(),
		ResponseDelay: 42 * time.Second,
	}
	raw := meta.encode()
	meta2 := decodeMetaData(raw)
	c.Assert(meta, DeepEquals, meta2)
}
