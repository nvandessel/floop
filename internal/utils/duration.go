// Package utils provides shared utility functions for the floop codebase.
package utils

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// maxDays is the maximum number of days representable without int64 overflow.
// time.Duration is nanoseconds in int64; max ~292 years = ~106,751 days.
const maxDays = 106751

// ParseDuration extends time.ParseDuration with support for day ("d") and
// week ("w") suffixes.
func ParseDuration(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid day duration %q: %w", s, err)
		}
		if days < 0 {
			return 0, fmt.Errorf("duration must be non-negative: %s", s)
		}
		if days > maxDays {
			return 0, fmt.Errorf("duration too large: %s (max %dd)", s, maxDays)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	if strings.HasSuffix(s, "w") {
		weeks, err := strconv.Atoi(strings.TrimSuffix(s, "w"))
		if err != nil {
			return 0, fmt.Errorf("invalid week duration %q: %w", s, err)
		}
		if weeks < 0 {
			return 0, fmt.Errorf("duration must be non-negative: %s", s)
		}
		if weeks > maxDays/7 {
			return 0, fmt.Errorf("duration too large: %s (max %dw)", s, maxDays/7)
		}
		return time.Duration(weeks) * 7 * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, fmt.Errorf("duration must be non-negative: %s", s)
	}
	return d, nil
}
