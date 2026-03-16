package utils

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"standard hours", "24h", 24 * time.Hour, false},
		{"standard minutes", "30m", 30 * time.Minute, false},
		{"days", "7d", 7 * 24 * time.Hour, false},
		{"weeks", "2w", 14 * 24 * time.Hour, false},
		{"zero days", "0d", 0, false},
		{"zero weeks", "0w", 0, false},
		{"one day", "1d", 24 * time.Hour, false},
		{"negative days", "-7d", 0, true},
		{"negative weeks", "-2w", 0, true},
		{"negative standard", "-5h", 0, true},
		{"overflow days", "999999d", 0, true},
		{"overflow weeks", "999999w", 0, true},
		{"invalid suffix", "7x", 0, true},
		{"empty string", "", 0, true},
		{"just d", "d", 0, true},
		{"just w", "w", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDuration(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseDuration(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseDuration(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
