package util

import "runtime"

func ArrayToSet[T comparable](arr []T) map[T]struct{} {
	ret := make(map[T]struct{})
	for _, elem := range arr {
		ret[elem] = struct{}{}
	}
	return ret
}

// GetNumCPUs is a helper function that returns the number of CPUs available
// to process.  This returns the value of [runtime.NumCPU] if it is greater
// than 0, otherwise it returns 1.
//
// NOTE: this function is provided to prevent the linting error for converting
// between an int and a uint.
func GetNumCPUs() uint64 {
	// We should always have at least one CPU core available, otherwise
	// how is this code even being run?
	return ConvertToUint64WithFallback[int, uint64](runtime.NumCPU(), 1)
}

// RangeInclusive represents a range of two values that is meant to be
// include both the start and end values when utilized.
type RangeInclusive[T Integer] struct {
	Start, End T
}

// CreateSliceOfIntegerForRangeInclusive creates a slice of integers from the given
// start to the end inclusive.
//
// The resulting slice will contain a sequence of integers starting at
// `start`, and ending at `endInclusive`, inclusive.
//
// Comprehension: [x | x ∈ [start, endInclusive]]
func CreateSliceOfIntegerForRangeInclusive[T Integer](start, endInclusive T) []T {
	if start > endInclusive {
		return nil
	}

	len := int(endInclusive - start + 1)
	result := make([]T, 0, len)

	for i := start; i <= endInclusive; i++ {
		result = append(result, i)
	}

	return result
}
