package gateway

import "math"

func clampNonNegativeIntToInt32(v int) int32 {
	if v < 0 {
		return 0
	}
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	// #nosec G115 -- bounded by checks above
	return int32(v)
}

func clampNonNegativeInt64ToInt32(v int64) int32 {
	if v < 0 {
		return 0
	}
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	// #nosec G115 -- bounded by checks above
	return int32(v)
}
