package util

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
