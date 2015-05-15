package cache

import (
	"fmt"
)

// Division operation is needed, so use float64 instead of int64
type ByteSize float64

const (
	B ByteSize = 1 << (10 * iota)
	KB
	MB
	GB
	TB
	PB
	EB
	ZB
	YB
)

func (b ByteSize) String() string {
	switch {
	case b >= YB:
		return fmt.Sprintf("%.2f YB", b/YB)
	case b >= ZB:
		return fmt.Sprintf("%.2f ZB", b/ZB)
	case b >= EB:
		return fmt.Sprintf("%.2f EB", b/EB)
	case b >= PB:
		return fmt.Sprintf("%.2f PB", b/PB)
	case b >= TB:
		return fmt.Sprintf("%.2f TB", b/TB)
	case b >= GB:
		return fmt.Sprintf("%.2f GB", b/GB)
	case b >= MB:
		return fmt.Sprintf("%.2f MB", b/MB)
	case b >= KB:
		return fmt.Sprintf("%.2f KB", b/KB)
	}
	return fmt.Sprintf("%g bytes", b)
}
