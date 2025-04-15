package espressostreamer

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
	result := -1

	if arr == nil || len(*arr) == 0 {
		return result
	}

	j := 0
	for i := 0; i < len(*arr); i++ {
		result := compareFunc((*arr)[i])

		if result == FilterAndFind_Remove {
			continue
		}

		// Take the first element that matches
		if result == FilterAndFind_Target && !hasFound {
			hasFound = true
			result = i
			j++
		} else if result == FilterAndFind_Keep {
			if i != j {
				(*arr)[j] = (*arr)[i]
			}
			j++
		}
	}

	*arr = (*arr)[:j]
	return result
}
