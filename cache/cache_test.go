package cache

import (
	"testing"

	. "gopkg.in/check.v1"
)

type MySuite struct{}

var _ = Suite(&MySuite{})

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }
