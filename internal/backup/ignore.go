package backup

import (
	"bufio"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type Pattern struct {
	raw        string
	pattern    string
	isNegation bool
	isDirOnly  bool
	isRooted   bool
	Source     string // e.g. .gitignore, .backupignore
}

type IgnoreMatcher struct {
	patterns []Pattern
	parent   *IgnoreMatcher
	dir      string
}

func NewIgnoreMatcher(dir string, parent *IgnoreMatcher) *IgnoreMatcher {
	return &IgnoreMatcher{
		dir:    dir,
		parent: parent,
	}
}

func (m *IgnoreMatcher) LoadIgnoreFiles() error {
	// Priority: .backupignore > .gitignore
	// User said "use them interchangeably". Let's load .gitignore then .backupignore, appending patterns.
	// Later patterns override earlier ones in the same list.
	// If valid, we append to m.patterns

	files := []string{".gitignore", ".backupignore"}
	for _, f := range files {
		path := filepath.Join(m.dir, f)
		if _, err := os.Stat(path); err == nil {
			if err := m.loadFile(path, f); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *IgnoreMatcher) loadFile(path, filename string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		p := Pattern{raw: line, Source: filename}

		if strings.HasPrefix(line, "!") {
			p.isNegation = true
			line = line[1:]
		}

		if strings.HasSuffix(line, "/") {
			p.isDirOnly = true
			line = line[:len(line)-1]
		}

		if strings.HasPrefix(line, "/") {
			p.isRooted = true
			line = line[1:]
		}

		p.pattern = line
		m.patterns = append(m.patterns, p)
	}
	return scanner.Err()
}

// Match returns (shouldIgnore, matchedPattern).
// shouldIgnore is true if the file should be ignored.
// matchedPattern is the pattern that caused the ignore (or un-ignore).
func (m *IgnoreMatcher) Match(path string, isDir bool) (bool, *Pattern) {
	// Calculate path relative to m.dir
	relPath, err := filepath.Rel(m.dir, path)
	if err != nil {
		return false, nil // Should not happen if path is inside m.dir
	}
	relPath = filepath.ToSlash(relPath)

	// Check local patterns in reverse order
	for i := len(m.patterns) - 1; i >= 0; i-- {
		p := m.patterns[i]

		if p.isDirOnly && !isDir {
			continue
		}

		match := false

		// Simple glob matching for now.
		// "foo" matches "foo", "bar/foo"
		// "/foo" matches "foo" only (anchored)
		// "foo/*.txt" ...

		if p.isRooted {
			// Match from root of this matcher
			// relPath must match pattern exactly or via glob
			if m.globMatch(p.pattern, relPath) {
				match = true
			}
		} else {
			// Match anywhere
			// If pattern contains /, it is relative to root?
			// Gitignore: "If the pattern ends with a slash, it is removed for the purpose of the following description, but it would only find a match with a directory. In other respects, it checks for a match between the pathname and the pattern."
			// "If the pattern does not contain a slash /, Git treats it as a shell glob pattern and checks for a match against the pathname relative to the location of the .gitignore file (relative to the toplevel of the working tree if not from a .gitignore file)."
			// "Otherwise, Git treats the pattern as a shell glob suitable for consumption by fnmatch(3) with the FNM_PATHNAME flag: wildcards in the pattern will not match a / in the pathname."

			if strings.Contains(p.pattern, "/") {
				// Contains slash -> relative to root (effectively anchored, but allows wildcards)
				// e.g. "foo/bar" matches "foo/bar" but not "a/foo/bar"
				// So it acts like Rooted?
				// Yes, "If the pattern contains a slash, ... it matches relative to the .gitignore file"
				if m.globMatch(p.pattern, relPath) {
					match = true
				}
			} else {
				// No slash -> match matching filename in any subdirectory?
				// e.g. "foo" matches "foo", "a/foo", "a/b/foo"
				// We need to check if the basename matches, OR if any path component matches?
				// Actually typically we check if the file name matches.
				if m.globMatch(p.pattern, filepath.Base(relPath)) {
					match = true
				}
				// What if pattern is "*.o"? Matches "a.o", "b/c.o"
				// Yes, Base check covers this.
				// Wait, "doc/*.txt" has slash, so it falls in previous block.
			}
		}

		if match {
			if p.isNegation {
				return false, &p // Explicitly included
			}
			return true, &p // Explicitly ignored
		}
	}

	if m.parent != nil {
		return m.parent.Match(path, isDir)
	}

	return false, nil
}

func (m *IgnoreMatcher) globMatch(pattern, name string) bool {
	matched, _ := path.Match(pattern, name)
	return matched
}
