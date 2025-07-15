package espressostreamer

import (
	"fmt"
)

const (
	FilterAndFind_Remove = iota
	FilterAndFind_Keep
	FilterAndFind_Target
)

// FilterAndFind filters an array in-place and returns the matching element based on a comparison function.
// The comparison function should return:
//   - FilterAndFindTarget for the element to be returned, will be kept in the array
//   - FilterAndFindKeep for elements to be kept
//   - FilterAndFindRemove for elements to be removed
//
// Returns the index of the found element (if any)
func FilterAndFind[T any](arr *[]T, compareFunc func(T) int) int {

	var hasFound bool
	idx := -1

	if arr == nil || len(*arr) == 0 {
		return idx
	}

	// `j` is the next legal index to insert an element
	j := 0
	for i := 0; i < len(*arr); i++ {
		result := compareFunc((*arr)[i])

		if result == FilterAndFind_Remove || (result == FilterAndFind_Target && hasFound) {
			// here we skip the element and do not increment `j`
			continue
		}

		// Take the first element that matches
		if result == FilterAndFind_Target {
			hasFound = true
			idx = j
		}
		if i != j {
			// current element should be kept, so we move it to the next legal index `j`.
			(*arr)[j] = (*arr)[i]
		}
		j++
	}

	// now `j` is the length of elements to keep, we truncate the array to the new length
	*arr = (*arr)[:j]
	return idx
}

// CountUniqueEntries iterates over an array with potential duplicate values and counts the unique entries.
// returns a Uint that represents the number of unique entries.
// @Dev:
func CountUniqueEntries[T any](arr *[]T) uint64 {
	var uniqueCount uint64 // Declare the variable before assignment so the compiler doesn't infer it as an int.
	entriesMap := make(map[any]bool)
	uniqueCount = 0
	for _, entry := range *arr {
		if !entriesMap[entry] {
			uniqueCount += 1
			entriesMap[entry] = true
		}

	}
	return uniqueCount
}

// Error types

type EphemeralError struct {
	Err error
}

func (e *EphemeralError) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *EphemeralError) Unwrap() error {
	return e.Err
}

type PersistentError struct {
	Err error
}

type rpcError interface {
	ErrorData() interface{}
}

func (c *PersistentError) Error() string {
	if c.Err == nil {
		return ""
	}
	return c.Err.Error()
}

func (c *PersistentError) Unwrap() error {
	return c.Err
}

// ErrorData makes PersistentError satisfy an interface that has an ErrorData method.
// Its presence signals that this is a persistent error that should not be retried.
func (c *PersistentError) ErrorData() interface{} {
	// We only need to implement the method to satisfy the interface for type assertions.
	// The actual error data, if it exists, is in the wrapped error.
	// We return nil because this specific wrapper doesn't add any new data itself.
	return nil
}

// Error type helpers

func NewEphemeralError(format string, a ...any) error {
	return &EphemeralError{Err: fmt.Errorf(format, a...)}
}

func NewPersistentError(format string, a ...any) error {
	return &PersistentError{Err: fmt.Errorf(format, a...)}
}

func classifyVerificationError(err error) error {
	_, ok := err.(rpcError)
	if ok {
		// The presence of error data indicates a contract revert, which is a persistent error.
		return NewPersistentError("verifying legacy attestation failed with a contract revert: %w", err)
	}

	// If there's no error data, it's likely a transient network or RPC issue.
	return NewEphemeralError("verifying legacy attestation failed: %w", err)
}
