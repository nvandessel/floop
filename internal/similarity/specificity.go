package similarity

// IsMoreSpecific returns true if a has all of b's conditions plus additional ones.
// a is more specific than b if:
//  1. a has more conditions than b
//  2. a includes all of b's conditions with the same values
//
// Empty b means "unscoped" (applies everywhere); a scoped behavior is not
// a specialization of an unscoped one, so this returns false.
func IsMoreSpecific(a, b map[string]interface{}) bool {
	if len(a) <= len(b) {
		return false
	}

	// Empty when means "unscoped" (applies everywhere), not "less specific".
	// A scoped behavior is not a specialization of an unscoped one.
	if len(b) == 0 {
		return false
	}

	for key, valueB := range b {
		valueA, exists := a[key]
		if !exists {
			return false
		}
		if !ValuesEqual(valueA, valueB) {
			return false
		}
	}

	return true
}
