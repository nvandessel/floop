package models

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContextSnapshot_Matches(t *testing.T) {
	tests := []struct {
		name      string
		context   ContextSnapshot
		predicate map[string]interface{}
		want      bool
	}{
		{
			name: "exact match on language",
			context: ContextSnapshot{
				FileLanguage: "go",
			},
			predicate: map[string]interface{}{
				"language": "go",
			},
			want: true,
		},
		{
			name: "no match on language",
			context: ContextSnapshot{
				FileLanguage: "python",
			},
			predicate: map[string]interface{}{
				"language": "go",
			},
			want: false,
		},
		{
			name: "match with multiple conditions",
			context: ContextSnapshot{
				FileLanguage: "go",
				Task:         "refactor",
				Environment:  "dev",
			},
			predicate: map[string]interface{}{
				"language": "go",
				"task":     "refactor",
			},
			want: true,
		},
		{
			name: "partial match fails - all conditions required",
			context: ContextSnapshot{
				FileLanguage: "go",
				Task:         "write",
			},
			predicate: map[string]interface{}{
				"language": "go",
				"task":     "refactor",
			},
			want: false,
		},
		{
			name: "glob pattern match - filename only",
			context: ContextSnapshot{
				FilePath: "behavior.go",
			},
			predicate: map[string]interface{}{
				"file_path": "*.go",
			},
			want: true,
		},
		{
			name: "glob pattern no match",
			context: ContextSnapshot{
				FilePath: "behavior.go",
			},
			predicate: map[string]interface{}{
				"file_path": "*.py",
			},
			want: false,
		},
		{
			name: "glob pattern - full path doesn't match simple glob",
			context: ContextSnapshot{
				FilePath: "internal/models/behavior.go",
			},
			predicate: map[string]interface{}{
				"file_path": "*.go",
			},
			want: false, // filepath.Match("*.go", "internal/models/behavior.go") = false
		},
		{
			name: "array membership match",
			context: ContextSnapshot{
				Task: "refactor",
			},
			predicate: map[string]interface{}{
				"task": []interface{}{"write", "refactor", "review"},
			},
			want: true,
		},
		{
			name: "array membership no match",
			context: ContextSnapshot{
				Task: "deploy",
			},
			predicate: map[string]interface{}{
				"task": []interface{}{"write", "refactor", "review"},
			},
			want: false,
		},
		{
			name: "string array membership match",
			context: ContextSnapshot{
				Task: "refactor",
			},
			predicate: map[string]interface{}{
				"task": []string{"write", "refactor", "review"},
			},
			want: true,
		},
		{
			name: "empty predicate matches everything",
			context: ContextSnapshot{
				FileLanguage: "go",
				Task:         "write",
			},
			predicate: map[string]interface{}{},
			want:      true,
		},
		{
			name:    "nil actual value fails",
			context: ContextSnapshot{
				// Task is empty
			},
			predicate: map[string]interface{}{
				"task": "write",
			},
			want: false,
		},
		{
			name: "alternate field name - env",
			context: ContextSnapshot{
				Environment: "prod",
			},
			predicate: map[string]interface{}{
				"env": "prod",
			},
			want: true,
		},
		{
			name: "custom field match",
			context: ContextSnapshot{
				Custom: map[string]interface{}{
					"team": "backend",
				},
			},
			predicate: map[string]interface{}{
				"team": "backend",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.context.Matches(tt.predicate)
			if got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInferLanguage(t *testing.T) {
	tests := []struct {
		filePath string
		want     string
	}{
		{"main.go", "go"},
		{"script.py", "python"},
		{"app.js", "javascript"},
		{"component.ts", "typescript"},
		{"lib.rs", "rust"},
		{"app.rb", "ruby"},
		{"Main.java", "java"},
		{"main.c", "c"},
		{"header.h", "c"},
		{"main.cpp", "cpp"},
		{"main.cc", "cpp"},
		{"header.hpp", "cpp"},
		{"README.md", "markdown"},
		{"config.yaml", "yaml"},
		{"config.yml", "yaml"},
		{"data.json", "json"},
		{"unknown.xyz", ""},
		{"noextension", ""},
		{"path/to/file.go", "go"},
		{"FILE.GO", "go"}, // case insensitive
	}

	for _, tt := range tests {
		t.Run(tt.filePath, func(t *testing.T) {
			got := InferLanguage(tt.filePath)
			if got != tt.want {
				t.Errorf("InferLanguage(%q) = %q, want %q", tt.filePath, got, tt.want)
			}
		})
	}
}

func TestMatchValue(t *testing.T) {
	tests := []struct {
		name     string
		actual   interface{}
		required interface{}
		want     bool
	}{
		{"nil actual", nil, "value", false},
		{"exact string match", "hello", "hello", true},
		{"string mismatch", "hello", "world", false},
		{"glob match star", "test.go", "*.go", true},
		{"glob no match", "test.go", "*.py", false},
		{"non-string actual with string required", 123, "123", false},
		{"interface array match", "b", []interface{}{"a", "b", "c"}, true},
		{"interface array no match", "d", []interface{}{"a", "b", "c"}, false},
		{"string array match", "b", []string{"a", "b", "c"}, true},
		{"string array no match", "d", []string{"a", "b", "c"}, false},
		{"non-string actual with array", 123, []interface{}{"a", "b"}, false},
		{"equal non-string values", 42, 42, true},
		{"unequal non-string values", 42, 43, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchValue(tt.actual, tt.required)
			if got != tt.want {
				t.Errorf("matchValue(%v, %v) = %v, want %v", tt.actual, tt.required, got, tt.want)
			}
		})
	}
}

func TestInferLanguageFromContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		// Shebang tests
		{
			name:    "python shebang with env",
			content: "#!/usr/bin/env python\nprint('hello')",
			want:    "python",
		},
		{
			name:    "python3 shebang with env",
			content: "#!/usr/bin/env python3\nimport sys",
			want:    "python",
		},
		{
			name:    "node shebang",
			content: "#!/usr/bin/env node\nconsole.log('hello')",
			want:    "javascript",
		},
		{
			name:    "bash shebang",
			content: "#!/bin/bash\necho hello",
			want:    "shell",
		},
		{
			name:    "sh shebang",
			content: "#!/bin/sh\necho hello",
			want:    "shell",
		},
		{
			name:    "env sh shebang",
			content: "#!/usr/bin/env sh\necho hello",
			want:    "shell",
		},
		{
			name:    "ruby shebang",
			content: "#!/usr/bin/env ruby\nputs 'hello'",
			want:    "ruby",
		},
		{
			name:    "perl shebang",
			content: "#!/usr/bin/perl\nprint 'hello'",
			want:    "perl",
		},
		// Pattern tests
		{
			name:    "go package declaration",
			content: "package main\n\nfunc main() {}",
			want:    "go",
		},
		{
			name:    "go package with comment",
			content: "// Comment\npackage models",
			want:    "go",
		},
		{
			name:    "python def function",
			content: "def hello():\n    print('hi')",
			want:    "python",
		},
		{
			name:    "python class",
			content: "class MyClass:\n    pass",
			want:    "python",
		},
		{
			name:    "rust fn main",
			content: "fn main() {\n    println!(\"hello\");\n}",
			want:    "rust",
		},
		{
			name:    "javascript function",
			content: "function hello() {\n    console.log('hi');\n}",
			want:    "javascript",
		},
		{
			name:    "javascript const",
			content: "const x = 5;\nlet y = 10;",
			want:    "javascript",
		},
		// No match
		{
			name:    "empty content",
			content: "",
			want:    "",
		},
		{
			name:    "plain text",
			content: "This is just some text without any language markers.",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferLanguageFromContent(tt.content)
			if got != tt.want {
				t.Errorf("InferLanguageFromContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInferProjectType(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(dir string) error
		want    ProjectType
		wantErr bool
	}{
		{
			name: "go project",
			setup: func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)
			},
			want: ProjectTypeGo,
		},
		{
			name: "node project",
			setup: func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0644)
			},
			want: ProjectTypeNode,
		},
		{
			name: "python project with requirements.txt",
			setup: func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask"), 0644)
			},
			want: ProjectTypePython,
		},
		{
			name: "python project with pyproject.toml",
			setup: func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[project]"), 0644)
			},
			want: ProjectTypePython,
		},
		{
			name: "rust project",
			setup: func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]"), 0644)
			},
			want: ProjectTypeRust,
		},
		{
			name: "empty directory",
			setup: func(dir string) error {
				return nil
			},
			want: ProjectTypeUnknown,
		},
		{
			name: "go takes priority over others",
			setup: func(dir string) error {
				os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)
				return os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0644)
			},
			want: ProjectTypeGo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory
			dir, err := os.MkdirTemp("", "projecttype_test")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(dir)

			// Setup the test files
			if err := tt.setup(dir); err != nil {
				t.Fatalf("Setup failed: %v", err)
			}

			got := InferProjectType(dir)
			if got != tt.want {
				t.Errorf("InferProjectType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProjectType_String(t *testing.T) {
	tests := []struct {
		pt   ProjectType
		want string
	}{
		{ProjectTypeGo, "go"},
		{ProjectTypeNode, "node"},
		{ProjectTypePython, "python"},
		{ProjectTypeRust, "rust"},
		{ProjectTypeUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(string(tt.pt), func(t *testing.T) {
			if string(tt.pt) != tt.want {
				t.Errorf("ProjectType = %v, want %v", string(tt.pt), tt.want)
			}
		})
	}
}
