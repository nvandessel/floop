package consolidation

// nullIfEmpty returns nil (SQL NULL) for empty strings, or the string itself.
func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// nullIfZero returns nil (SQL NULL) for zero values, or the int itself.
func nullIfZero(n int) interface{} {
	if n == 0 {
		return nil
	}
	return n
}
