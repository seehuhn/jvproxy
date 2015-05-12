package cache

import (
	. "gopkg.in/check.v1"
	"net/http"
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
