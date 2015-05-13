package cache

import (
	. "gopkg.in/check.v1"
	"math"
)

func (s *MySuite) TestCandidates(c *C) {
	p := make(candidates, 0, pruneChunkSize)
	k := pruneChunkSize / 3
	for i := 1; i < k; i++ {
		p = p.add(nil, 0, float64(i))
	}
	for i := 2*k - 1; i >= k; i-- {
		p = p.add(nil, 0, float64(i))
	}
	for i := 2 * k; i < pruneChunkSize+1; i++ {
		p = p.add(nil, 0, float64(i))
	}
	p = p.add(nil, 0, float64(0))

	c.Assert(len(p), Equals, pruneChunkSize)

	for i, x := range p {
		c.Assert(math.Abs(x.score-float64(pruneChunkSize-i)) <= 1e-5, Equals, true)
	}
}
