package espressostreamer

// FilterAndFind filters an array in-place and returns the matching element based on a comparison function.
// The comparison function should return:
//   - 0 for the element to be returned
//   - negative number for elements to be removed
//   - positive number for elements to be kept
//
// Returns the found element (if any) and a boolean indicating if an element was found.
func FilterAndFind[T any](arr *[]T, compareFunc func(T) int) (T, bool) {
	var found T
	var hasFound bool

	if arr == nil || len(*arr) == 0 {
		return found, false
	}

	j := 0
	for i := 0; i < len(*arr); i++ {
		result := compareFunc((*arr)[i])
		// Take the first element that matches
		if result == 0 && !hasFound {
			found = (*arr)[i]
			hasFound = true
		} else if result > 0 {
			if i != j {
				(*arr)[j] = (*arr)[i]
			}
			j++
		}
	}

	*arr = (*arr)[:j]
	return found, hasFound
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
