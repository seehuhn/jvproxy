package cache

import (
	"net/http"

	. "gopkg.in/check.v1"
)

func (s *MySuite) TestVary(c *C) {
	fields := getVaryFields(nil)
	c.Assert(fields, HasLen, 0)

	h := http.Header{}
	h.Set("Vary", "Test")
	fields = getVaryFields(h)
	c.Assert(fields, DeepEquals, []string{"Test"})

	h = http.Header{}
	h.Set("Vary", "test")
	fields = getVaryFields(h)
	c.Assert(fields, DeepEquals, []string{"Test"})

	h = http.Header{}
	h.Set("Vary", "field-c, field-a")
	h.Add("Vary", "field-b")
	c.Assert(h["Vary"], HasLen, 2)
	fields = getVaryFields(h)
	c.Assert(fields, DeepEquals, []string{"Field-A", "Field-B", "Field-C"})
}
