package cache

import (
	"fmt"
)

type byteSize float64

// Division operation is needed, so use float64 instead of int64

const (
	_ byteSize = 1 << (10 * iota)
	kb
	mb
	gb
	tb
	pbt
	eb
	zb
	yb
)

func (x byteSize) String() string {
	switch {
	case x >= yb:
		return fmt.Sprintf("%.2f YB", x/yb)
	case x >= zb:
		return fmt.Sprintf("%.2f ZB", x/zb)
	case x >= eb:
		return fmt.Sprintf("%.2f EB", x/eb)
	case x >= pbt:
		return fmt.Sprintf("%.2f PB", x/pbt)
	case x >= tb:
		return fmt.Sprintf("%.2f TB", x/tb)
	case x >= gb:
		return fmt.Sprintf("%.2f GB", x/gb)
	case x >= mb:
		return fmt.Sprintf("%.2f MB", x/mb)
	case x >= kb:
		return fmt.Sprintf("%.2f KB", x/kb)
	}
	return fmt.Sprintf("%g bytes", x)
}
