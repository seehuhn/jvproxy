package cache

import (
	. "gopkg.in/check.v1"
	"testing"
)

type MySuite struct{}

var _ = Suite(&MySuite{})

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }
