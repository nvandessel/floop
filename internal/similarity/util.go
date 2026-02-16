package similarity

// ValuesEqual compares two interface{} values for equality.
// Handles string, []interface{}, and []string comparisons.
// For slices, returns true if there is at least one common element.
func ValuesEqual(a, b interface{}) bool {
	// Handle string comparison
	aStr, aIsStr := a.(string)
	bStr, bIsStr := b.(string)
	if aIsStr && bIsStr {
		return aStr == bStr
	}

	// Handle slice comparison (both must contain at least one common element)
	aSlice, aIsSlice := a.([]interface{})
	bSlice, bIsSlice := b.([]interface{})
	if aIsSlice && bIsSlice {
		for _, av := range aSlice {
			for _, bv := range bSlice {
				if ValuesEqual(av, bv) {
					return true
				}
			}
		}
		return false
	}

	// Handle string slice comparison
	aStrSlice, aIsStrSlice := a.([]string)
	bStrSlice, bIsStrSlice := b.([]string)
	if aIsStrSlice && bIsStrSlice {
		for _, av := range aStrSlice {
			for _, bv := range bStrSlice {
				if av == bv {
					return true
				}
			}
		}
		return false
	}

	// Fallback to direct equality
	return a == b
}
