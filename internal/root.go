package internal

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type BackupRoot struct {
	b          *Backup
	Time       time.Time
	BackupHead string
	hash       string
}

func NewBackupRoot(b *Backup, headPath string) (*BackupRoot, error) {
	name := filepath.Base(headPath)
	// Format: yyMMdd-HHmmss
	t, err := time.ParseInLocation("060102-150405", name, time.Local)
	if err != nil {
		return nil, err
	}
	// Validate content (must not be empty)
	content, err := ioutil.ReadFile(headPath)
	if err != nil {
		return nil, err
	}
	hash := strings.TrimSpace(string(content))
	if len(hash) == 0 {
		return nil, fmt.Errorf("snapshot file is empty")
	}

	return &BackupRoot{
		b:          b,
		Time:       t,
		BackupHead: headPath,
		hash:       hash,
	}, nil
}

func (r *BackupRoot) String() string {
	name := r.Time.Format("060102-150405")
	if r.b.ProjectName == "" {
		// Headless: check if we are in a subdirectory of StoreSnapshots
		// Structure: .../snapshots/<project>/<timestamp>
		dir := filepath.Dir(r.BackupHead)
		parent := filepath.Dir(dir)
		if filepath.Base(parent) == "snapshots" {
			projectName := filepath.Base(dir)
			return filepath.Join(projectName, name)
		}
	}
	return name
}

func (r *BackupRoot) Hash() (string, error) {
	if r.hash != "" {
		return r.hash, nil
	}
	content, err := ioutil.ReadFile(r.BackupHead)
	if err != nil {
		return "", err
	}
	r.hash = strings.TrimSpace(string(content))
	return r.hash, nil
}

func (b *Backup) ListProjects() ([]string, error) {
	var projects []string
	entries, err := os.ReadDir(b.StoreSnapshots)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			projects = append(projects, entry.Name())
		}
	}
	return projects, nil
}

func (r *BackupRoot) TopDirectory() (*BackupDirectory, error) {
	h, err := r.Hash()
	if err != nil {
		return nil, err
	}
	// Top directory name is "." or based on context?
	// Actually, BackupRoot represents the root, so its top directory is the contents of the backup root hash.
	// We can give it a name like "."
	return r.b.BackupDirectory(h, "."), nil
}

// LocateDirectory finds a directory inside the backup.
// fullName is the relative path from the top of the backup.
func (r *BackupRoot) LocateDirectory(fullName string) (*BackupDirectory, error) {
	if fullName == "" || fullName == "." || fullName == string(os.PathSeparator) {
		return r.TopDirectory()
	}

	current, err := r.TopDirectory()
	if err != nil {
		return nil, err
	}

	// Split path into components
	// Handle cross-OS paths by normalizing separators:
	// 1. Replace all backslashes with forward slashes to handle Windows paths on Linux
	// 2. Use filepath.Clean to resolve . and .. and normalize separators to local OS
	normalized := strings.ReplaceAll(fullName, "\\", "/")
	cleanPath := filepath.Clean(normalized)

	if cleanPath == "." {
		return current, nil
	}

	parts := strings.Split(cleanPath, string(os.PathSeparator))

	for _, part := range parts {
		if part == "" {
			continue
		}
		entries, err := current.Entries()
		if err != nil {
			return nil, err
		}

		entry, ok := entries[part]
		if !ok {
			return nil, nil // Not found
		}

		dirEntry, ok := entry.(*BackupDirectory)
		if !ok {
			return nil, nil // Found but it is a file
		}
		current = dirEntry
	}

	return current, nil
}

// Locate finds an entry (file or directory) inside the backup.
func (r *BackupRoot) Locate(fullName string) (BackupEntry, error) {
	if fullName == "" || fullName == "." || fullName == string(os.PathSeparator) {
		return r.TopDirectory()
	}

	current, err := r.TopDirectory()
	if err != nil {
		return nil, err
	}

	// Normalize separators to handle cross-OS paths:
	// 1. Replace all backslashes with forward slashes
	// 2. Use filepath.Clean to resolve . and .. and normalize separators to local OS
	normalized := strings.ReplaceAll(fullName, "\\", "/")
	cleanPath := filepath.Clean(normalized)
	if cleanPath == "." {
		return current, nil
	}

	parts := strings.Split(cleanPath, string(os.PathSeparator))

	for i, part := range parts {
		if part == "" {
			continue
		}
		entries, err := current.Entries()
		if err != nil {
			return nil, err
		}

		entry, ok := entries[part]
		if !ok {
			return nil, nil // Not found
		}

		// If this is the last part, return the entry
		if i == len(parts)-1 {
			return entry, nil
		}

		// Otherwise, it must be a directory to continue
		dirEntry, ok := entry.(*BackupDirectory)
		if !ok {
			return nil, nil // Found path prefix but it is a file, cannot traverse
		}
		current = dirEntry
	}

	return current, nil
}

type BackupRoots []*BackupRoot

func (s BackupRoots) Len() int           { return len(s) }
func (s BackupRoots) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s BackupRoots) Less(i, j int) bool { return s[i].Time.Before(s[j].Time) }
