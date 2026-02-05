// Package utils provides utility functions for the feedback-loop project.
package utils

// GetString safely extracts a string from a map, returning defaultVal if not found or wrong type.
func GetString(m map[string]interface{}, key, defaultVal string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return defaultVal
}

// GetStringSlice safely extracts a string slice from a map.
// Handles both []string and []interface{} cases.
func GetStringSlice(m map[string]interface{}, key string) []string {
	if v, ok := m[key].([]string); ok {
		return v
	}
	// Handle []interface{} case (common from JSON unmarshaling)
	if v, ok := m[key].([]interface{}); ok {
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

// GetFloat64 safely extracts a float64 from a map.
func GetFloat64(m map[string]interface{}, key string, defaultVal float64) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return defaultVal
}

// GetInt safely extracts an int from a map.
// Also handles float64 (common from JSON) by converting.
func GetInt(m map[string]interface{}, key string, defaultVal int) int {
	if v, ok := m[key].(int); ok {
		return v
	}
	// JSON numbers are float64
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return defaultVal
}

// GetMap safely extracts a nested map from a map.
func GetMap(m map[string]interface{}, key string) map[string]interface{} {
	if v, ok := m[key].(map[string]interface{}); ok {
		return v
	}
	return nil
}

// GetBool safely extracts a bool from a map.
func GetBool(m map[string]interface{}, key string, defaultVal bool) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return defaultVal
}
