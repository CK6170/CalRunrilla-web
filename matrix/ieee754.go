package matrix

import (
	"math"
)

func ToIEEE754(f float32) uint32 {
	return math.Float32bits(f)
}
