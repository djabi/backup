package internal

import (
	"path/filepath"
	"testing"
)

func TestIgnoreMatcher_Match(t *testing.T) {
	tests := []struct {
		name     string
		patterns []Pattern
		path     string
		isDir    bool
		want     bool
	}{
		{
			name: "match simple file",
			patterns: []Pattern{
				{pattern: "foo", raw: "foo"},
			},
			path: "foo",
			want: true,
		},
		{
			name: "match nested file",
			patterns: []Pattern{
				{pattern: "foo", raw: "foo"},
			},
			path: "bar/foo",
			want: true,
		},
		{
			name: "rooted match",
			patterns: []Pattern{
				{pattern: "foo", raw: "/foo", isRooted: true},
			},
			path: "foo",
			want: true,
		},
		{
			name: "rooted mismatch nested",
			patterns: []Pattern{
				{pattern: "foo", raw: "/foo", isRooted: true},
			},
			path: "bar/foo",
			want: false,
		},
		{
			name: "directory only match",
			patterns: []Pattern{
				{pattern: "foo", raw: "foo/", isDirOnly: true},
			},
			path:  "foo",
			isDir: true,
			want:  true,
		},
		{
			name: "directory only mismatch file",
			patterns: []Pattern{
				{pattern: "foo", raw: "foo/", isDirOnly: true},
			},
			path:  "foo",
			isDir: false,
			want:  false,
		},
		{
			name: "negation",
			patterns: []Pattern{
				{pattern: "*.log", raw: "*.log"},
				{pattern: "important.log", raw: "!important.log", isNegation: true},
			},
			path: "important.log",
			want: false,
		},
		{
			name: "negation override",
			patterns: []Pattern{
				{pattern: "important.log", raw: "!important.log", isNegation: true},
				{pattern: "*.log", raw: "*.log"},
			},
			path: "important.log",
			want: true,
		},
		{
			name: "glob match",
			patterns: []Pattern{
				{pattern: "*.txt", raw: "*.txt"},
			},
			path: "doc/file.txt",
			want: true,
		},
		{
			name: "slash match",
			patterns: []Pattern{
				{pattern: "doc/*.txt", raw: "doc/*.txt"},
			},
			path: "doc/file.txt",
			want: true,
		},
		{
			name: "slash mismatch deeper",
			patterns: []Pattern{
				{pattern: "doc/*.txt", raw: "doc/*.txt"},
			},
			path: "doc/sub/file.txt",
			want: false, // glob *.txt doesn't match sub/file.txt
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewIgnoreMatcher("/tmp/root", nil)
			m.patterns = tt.patterns

			// We simulate path being full path
			fullPath := filepath.Join("/tmp/root", tt.path)
			got, _ := m.Match(fullPath, tt.isDir)
			if got != tt.want {
				t.Errorf("Match(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestIgnoreMatcher_Parent(t *testing.T) {
	// Parent ignores "*.log"
	parent := NewIgnoreMatcher("/tmp/root", nil)
	parent.patterns = []Pattern{{pattern: "*.log", raw: "*.log"}}

	// Child ignores nothing but has parent
	child := NewIgnoreMatcher("/tmp/root/sub", parent)

	if got, _ := child.Match("/tmp/root/sub/test.log", false); !got {
		t.Error("Child should inherit parent ignore")
	}

	// Child negates specific log
	child.patterns = []Pattern{{pattern: "important.log", raw: "!important.log", isNegation: true}}

	// Note: Standard gitignore: "It is not possible to re-include a file if a parent directory of that file is excluded."
	// BUT here we are talking about pattern in parent directory vs pattern in child directory.
	// If parent dir says "*.log", and child dir has "!important.log", does it un-ignore?
	// In git, yes, patterns in deeper files override shallower ones.

	if got, _ := child.Match("/tmp/root/sub/important.log", false); got {
		t.Error("Child should be able to negate parent ignore")
	}
}
