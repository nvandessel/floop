package utils

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

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
		return time.Duration(weeks) * 7 * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}
