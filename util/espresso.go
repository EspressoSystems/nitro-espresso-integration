package util

import "runtime"

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

// SignedInteger represents any type that is a signed integer, or whose
// underlying type is a signed integer.
type SignedInteger interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}

// ConvertToUint64WithFallback is a helper function that converts a signed
// integer value to a type whose underlying type is an unsigned 64-bit integer,
// and returns a fallback value if the signed value is negative. If the value
func ConvertToUint64WithFallback[T SignedInteger, U ~uint64](value T, fallback U) U {
	if value < 0 {
		return fallback
	}

	return U(value)
}

// UnsignedInteger represents any type that is an unsigned integer, or whose
// underlying type is an unsigned integer.
type UnsignedInteger interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

// ConvertToInt64WithFallback is a helper function that converts an unsigned
// integer value to a type whose underlying type is a signed 64-bit integer,
// and returns a fallback value if the unsigned value is greater than the
// maximum value for a signed 64-bit integer.
func ConvertToInt64WithFallback[T UnsignedInteger, U ~int64](value T, fallback U) U {
	if uint64(value) > 0x7FFF_FFFF_FFFF_FFFF {
		return fallback
	}

	return U(value)
}

// Integer represents any type that is an Integer.  It specifies all types
// that are either directly Integer primitives, or a type that inherits their
// functionality via wrapping.
type Integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}
