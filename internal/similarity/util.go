package similarity

// toInterfaceSlice converts []interface{} or []string to []interface{}.
// Returns the slice and true if the value is a supported slice type.
func toInterfaceSlice(v interface{}) ([]interface{}, bool) {
	switch s := v.(type) {
	case []interface{}:
		return s, true
	case []string:
		out := make([]interface{}, len(s))
		for i, val := range s {
			out[i] = val
		}
		return out, true
	default:
		return nil, false
	}
}

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

	// Normalize []string to []interface{} so mixed types are handled uniformly
	aSlice, aIsSlice := toInterfaceSlice(a)
	bSlice, bIsSlice := toInterfaceSlice(b)
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

	// Fallback to direct equality
	return a == b
}
