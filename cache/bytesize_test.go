package cache

import (
	. "gopkg.in/check.v1"
)

func (s *MySuite) TestByteSize(c *C) {
	str := byteSize(1024).String()
	c.Assert(str, Equals, "1.00 KB")
	str = byteSize(1023).String()
	c.Assert(str, Equals, "1023 bytes")
}
