package utils

import (
	"testing"
)

func TestGetString(t *testing.T) {
	tests := []struct {
		name       string
		m          map[string]interface{}
		key        string
		defaultVal string
		want       string
	}{
		{
			name:       "key exists with string value",
			m:          map[string]interface{}{"name": "test"},
			key:        "name",
			defaultVal: "default",
			want:       "test",
		},
		{
			name:       "key does not exist",
			m:          map[string]interface{}{"other": "value"},
			key:        "name",
			defaultVal: "default",
			want:       "default",
		},
		{
			name:       "key exists but wrong type",
			m:          map[string]interface{}{"name": 123},
			key:        "name",
			defaultVal: "default",
			want:       "default",
		},
		{
			name:       "nil map",
			m:          nil,
			key:        "name",
			defaultVal: "default",
			want:       "default",
		},
		{
			name:       "empty string value",
			m:          map[string]interface{}{"name": ""},
			key:        "name",
			defaultVal: "default",
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetString(tt.m, tt.key, tt.defaultVal)
			if got != tt.want {
				t.Errorf("GetString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetStringSlice(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]interface{}
		key  string
		want []string
	}{
		{
			name: "key exists with []string value",
			m:    map[string]interface{}{"tags": []string{"a", "b", "c"}},
			key:  "tags",
			want: []string{"a", "b", "c"},
		},
		{
			name: "key exists with []interface{} containing strings",
			m:    map[string]interface{}{"tags": []interface{}{"a", "b", "c"}},
			key:  "tags",
			want: []string{"a", "b", "c"},
		},
		{
			name: "key exists with []interface{} mixed types",
			m:    map[string]interface{}{"tags": []interface{}{"a", 123, "c"}},
			key:  "tags",
			want: []string{"a", "c"},
		},
		{
			name: "key does not exist",
			m:    map[string]interface{}{"other": "value"},
			key:  "tags",
			want: nil,
		},
		{
			name: "key exists but wrong type",
			m:    map[string]interface{}{"tags": "not-a-slice"},
			key:  "tags",
			want: nil,
		},
		{
			name: "nil map",
			m:    nil,
			key:  "tags",
			want: nil,
		},
		{
			name: "empty slice",
			m:    map[string]interface{}{"tags": []string{}},
			key:  "tags",
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetStringSlice(tt.m, tt.key)
			if len(got) != len(tt.want) {
				t.Errorf("GetStringSlice() length = %v, want %v", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("GetStringSlice()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestGetFloat64(t *testing.T) {
	tests := []struct {
		name       string
		m          map[string]interface{}
		key        string
		defaultVal float64
		want       float64
	}{
		{
			name:       "key exists with float64 value",
			m:          map[string]interface{}{"confidence": 0.85},
			key:        "confidence",
			defaultVal: 0.5,
			want:       0.85,
		},
		{
			name:       "key does not exist",
			m:          map[string]interface{}{"other": 0.9},
			key:        "confidence",
			defaultVal: 0.5,
			want:       0.5,
		},
		{
			name:       "key exists but wrong type",
			m:          map[string]interface{}{"confidence": "high"},
			key:        "confidence",
			defaultVal: 0.5,
			want:       0.5,
		},
		{
			name:       "nil map",
			m:          nil,
			key:        "confidence",
			defaultVal: 0.5,
			want:       0.5,
		},
		{
			name:       "zero value",
			m:          map[string]interface{}{"confidence": 0.0},
			key:        "confidence",
			defaultVal: 0.5,
			want:       0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetFloat64(tt.m, tt.key, tt.defaultVal)
			if got != tt.want {
				t.Errorf("GetFloat64() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetInt(t *testing.T) {
	tests := []struct {
		name       string
		m          map[string]interface{}
		key        string
		defaultVal int
		want       int
	}{
		{
			name:       "key exists with int value",
			m:          map[string]interface{}{"priority": 5},
			key:        "priority",
			defaultVal: 0,
			want:       5,
		},
		{
			name:       "key exists with float64 value (JSON number)",
			m:          map[string]interface{}{"priority": float64(10)},
			key:        "priority",
			defaultVal: 0,
			want:       10,
		},
		{
			name:       "key does not exist",
			m:          map[string]interface{}{"other": 5},
			key:        "priority",
			defaultVal: 1,
			want:       1,
		},
		{
			name:       "key exists but wrong type",
			m:          map[string]interface{}{"priority": "high"},
			key:        "priority",
			defaultVal: 1,
			want:       1,
		},
		{
			name:       "nil map",
			m:          nil,
			key:        "priority",
			defaultVal: 1,
			want:       1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetInt(tt.m, tt.key, tt.defaultVal)
			if got != tt.want {
				t.Errorf("GetInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetMap(t *testing.T) {
	tests := []struct {
		name    string
		m       map[string]interface{}
		key     string
		wantNil bool
	}{
		{
			name: "key exists with map value",
			m: map[string]interface{}{
				"nested": map[string]interface{}{"inner": "value"},
			},
			key:     "nested",
			wantNil: false,
		},
		{
			name:    "key does not exist",
			m:       map[string]interface{}{"other": "value"},
			key:     "nested",
			wantNil: true,
		},
		{
			name:    "key exists but wrong type",
			m:       map[string]interface{}{"nested": "not-a-map"},
			key:     "nested",
			wantNil: true,
		},
		{
			name:    "nil map",
			m:       nil,
			key:     "nested",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetMap(tt.m, tt.key)
			if (got == nil) != tt.wantNil {
				t.Errorf("GetMap() nil = %v, wantNil %v", got == nil, tt.wantNil)
			}
		})
	}
}

func TestGetBool(t *testing.T) {
	tests := []struct {
		name       string
		m          map[string]interface{}
		key        string
		defaultVal bool
		want       bool
	}{
		{
			name:       "key exists with true value",
			m:          map[string]interface{}{"enabled": true},
			key:        "enabled",
			defaultVal: false,
			want:       true,
		},
		{
			name:       "key exists with false value",
			m:          map[string]interface{}{"enabled": false},
			key:        "enabled",
			defaultVal: true,
			want:       false,
		},
		{
			name:       "key does not exist",
			m:          map[string]interface{}{"other": true},
			key:        "enabled",
			defaultVal: true,
			want:       true,
		},
		{
			name:       "key exists but wrong type",
			m:          map[string]interface{}{"enabled": "yes"},
			key:        "enabled",
			defaultVal: false,
			want:       false,
		},
		{
			name:       "nil map",
			m:          nil,
			key:        "enabled",
			defaultVal: true,
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetBool(tt.m, tt.key, tt.defaultVal)
			if got != tt.want {
				t.Errorf("GetBool() = %v, want %v", got, tt.want)
			}
		})
	}
}
