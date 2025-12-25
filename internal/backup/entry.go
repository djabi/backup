package backup

import (
	"compress/gzip"
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type EntryType int

const (
	EntryTypeFile      EntryType = 0
	EntryTypeDirectory EntryType = 1
	EntryTypeLink      EntryType = 2
)

type Entry interface {
	Name() string
	Hash() (string, error)
	Save() error
	Type() EntryType
}

// FileEntry represents a file in the backup tree.
type FileEntry struct {
	b    *Backup
	path string
	name string
	hash string
}

func NewFileEntry(b *Backup, path string) (*FileEntry, error) {
	hash, err := b.HashCache.FileHash(path)
	if err != nil {
		return nil, err
	}
	return &FileEntry{
		b:    b,
		path: path,
		name: filepath.Base(path),
		hash: hash,
	}, nil
}

func (e *FileEntry) Name() string          { return e.name }
func (e *FileEntry) Type() EntryType       { return EntryTypeFile }
func (e *FileEntry) Hash() (string, error) { return e.hash, nil }

func (e *FileEntry) Save() error {
	e.b.Stats.FilesTotal++
	dest := e.b.Store.DataStore(e.hash)
	if dest == "" {
		return fmt.Errorf("invalid hash")
	}

	// Even in dry-run we want to check if it exists to know if we WOULD save it?
	// or simulate saving.
	if _, err := os.Stat(dest); err == nil {
		return nil // Already saved
	}

	e.b.Stats.FilesArchived++

	// Just for stats purposes we might want size?
	// But info.Size() is not readily available unless we call Stat again or store it in FileEntry.
	// We can trust the user doesn't need byte exact count for now
	// OR we can do a quick Stat here.
	if info, err := os.Stat(e.path); err == nil {
		e.b.Stats.BytesArchived += info.Size()
	}

	if e.b.DryRun {
		fmt.Printf("[dry-run] Would save file: %s -> %s\n", e.path, dest)
		return nil
	}

	relPath, _ := filepath.Rel(e.b.Top, e.path)
	fmt.Printf("Archiving: %s\n", relPath)

	tempDest := dest + ".partial"
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	// Gzip compress
	orig, err := os.Open(e.path)
	if err != nil {
		return err
	}
	defer orig.Close()

	out, err := os.Create(tempDest)
	if err != nil {
		return err
	}
	defer out.Close()

	gw := gzip.NewWriter(out)
	defer gw.Close()

	if _, err := io.Copy(gw, orig); err != nil {
		return err
	}
	if err := gw.Close(); err != nil {
		return err
	}

	return os.Rename(tempDest, dest)
}

// LinkEntry represents a symlink in the backup tree.
type LinkEntry struct {
	b      *Backup
	path   string
	name   string
	target string
	hash   string
}

func NewLinkEntry(b *Backup, path string) (*LinkEntry, error) {
	target, err := os.Readlink(path)
	if err != nil {
		return nil, err
	}
	hash := fmt.Sprintf("%x", md5.Sum([]byte(target)))
	return &LinkEntry{
		b:      b,
		path:   path,
		name:   filepath.Base(path),
		target: target,
		hash:   hash,
	}, nil
}

func (e *LinkEntry) Name() string          { return e.name }
func (e *LinkEntry) Type() EntryType       { return EntryTypeLink }
func (e *LinkEntry) Hash() (string, error) { return e.hash, nil }

func (e *LinkEntry) Save() error {
	e.b.Stats.FilesTotal++
	dest := e.b.Store.DataStore(e.hash)
	if dest == "" {
		return fmt.Errorf("invalid hash")
	}

	if _, err := os.Stat(dest); err == nil {
		return nil // Already saved
	}

	e.b.Stats.FilesArchived++

	if e.b.DryRun {
		fmt.Printf("[dry-run] Would save link: %s -> %s (target: %s)\n", e.path, dest, e.target)
		return nil
	}

	relPath, _ := filepath.Rel(e.b.Top, e.path)
	fmt.Printf("Archiving link: %s -> %s\n", relPath, e.target)

	tempDest := dest + ".partial"
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	out, err := os.Create(tempDest)
	if err != nil {
		return err
	}
	defer out.Close()

	gw := gzip.NewWriter(out)
	defer gw.Close()

	if _, err := gw.Write([]byte(e.target)); err != nil {
		return err
	}
	if err := gw.Close(); err != nil {
		return err
	}

	return os.Rename(tempDest, dest)
}

// DirectoryEntry represents a directory in the backup tree.
type IgnoredEntry struct {
	Path   string
	Name   string
	Reason *Pattern
}

// DirectoryEntry represents a directory in the backup tree.
type DirectoryEntry struct {
	b       *Backup
	path    string
	name    string
	hash    string
	content []Entry
	matcher *IgnoreMatcher
	ignored []IgnoredEntry
	scanned bool
}

func NewDirectoryEntry(b *Backup, path string, parentMatcher *IgnoreMatcher) *DirectoryEntry {
	// Create matcher for this directory
	m := NewIgnoreMatcher(path, parentMatcher)

	// Always try to load ignores
	m.LoadIgnoreFiles() // Ignore error

	return &DirectoryEntry{
		b:       b,
		path:    path,
		name:    filepath.Base(path),
		matcher: m,
	}
}

func (e *DirectoryEntry) Name() string    { return e.name }
func (e *DirectoryEntry) Type() EntryType { return EntryTypeDirectory }

func (e *DirectoryEntry) Content() ([]Entry, error) {
	if err := e.scan(); err != nil {
		return nil, err
	}
	return e.content, nil
}

func (e *DirectoryEntry) scan() error {
	if e.scanned {
		return nil
	}

	files, err := os.ReadDir(e.path)
	if err != nil {
		return nil // Return empty if error
		// return err
	}

	var entries []Entry
	var ignored []IgnoredEntry

	for _, f := range files {
		fullPath := filepath.Join(e.path, f.Name())
		isDir := f.IsDir()

		// Check ignores
		if e.matcher != nil {
			shouldIgnore, pattern := e.matcher.Match(fullPath, isDir)
			if shouldIgnore {
				ignored = append(ignored, IgnoredEntry{
					Path:   fullPath,
					Name:   f.Name(),
					Reason: pattern,
				})
				continue
			}
		}

		// Ignore symlinks?
		info, err := f.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			// Check ignores for symlink? Match(fullPath, false)?
			if e.matcher != nil {
				shouldIgnore, pattern := e.matcher.Match(fullPath, false)
				if shouldIgnore {
					ignored = append(ignored, IgnoredEntry{
						Path:   fullPath,
						Name:   f.Name(),
						Reason: pattern,
					})
					continue
				}
			}

			le, err := NewLinkEntry(e.b, fullPath)
			if err != nil {
				return err
			}
			entries = append(entries, le)
			continue
		}

		if f.Name() == ".backup" {
			continue
		}

		if f.IsDir() {
			// Pass THIS directory's matcher as parent
			entries = append(entries, NewDirectoryEntry(e.b, fullPath, e.matcher))
		} else {
			fe, err := NewFileEntry(e.b, fullPath)
			if err != nil {
				return err
			}
			entries = append(entries, fe)
		}
	}

	sort.Sort(&entrySorter{entries})

	e.content = entries
	e.ignored = ignored
	e.scanned = true
	return nil
}

func (e *DirectoryEntry) Ignored() ([]IgnoredEntry, error) {
	if err := e.scan(); err != nil {
		return nil, err
	}
	return e.ignored, nil
}

func (e *DirectoryEntry) Hash() (string, error) {
	if e.hash != "" {
		return e.hash, nil
	}

	content, err := e.ContentAsText()
	if err != nil {
		return "", err
	}

	h := md5.Sum([]byte(content))
	e.hash = fmt.Sprintf("%x", h)
	return e.hash, nil
}

func (e *DirectoryEntry) ContentAsText() (string, error) {
	entries, err := e.Content()
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for _, child := range entries {
		h, err := child.Hash()
		if err != nil {
			return "", err
		}

		typeChar := "F"
		if child.Type() == EntryTypeDirectory {
			typeChar = "D"
		} else if child.Type() == EntryTypeLink {
			typeChar = "L"
		}

		// Java: typeChar + " " + hash + " " + name + "\n"
		sb.WriteString(fmt.Sprintf("%s %s %s\n", typeChar, h, child.Name()))
	}
	return sb.String(), nil
}

func (e *DirectoryEntry) Save() error {
	e.b.Stats.DirsTotal++

	// First save all children
	children, err := e.Content()
	if err != nil {
		return err
	}
	for _, child := range children {
		if err := child.Save(); err != nil {
			return err
		}
	}

	// Now save directory content itself
	h, err := e.Hash()
	if err != nil {
		return err
	}

	dest := e.b.Store.DataStore(h)
	if dest == "" {
		return fmt.Errorf("invalid hash")
	}

	if _, err := os.Stat(dest); err == nil {
		return nil
	}

	e.b.Stats.DirsArchived++

	if e.b.DryRun {
		fmt.Printf("[dry-run] Would save directory listing: %s -> %s\n", e.path, dest)
		return nil
	}

	tempDest := dest + ".partial"
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	out, err := os.Create(tempDest)
	if err != nil {
		return err
	}
	defer out.Close()

	gw := gzip.NewWriter(out)
	defer gw.Close()

	content, err := e.ContentAsText()
	if err != nil {
		return err
	}

	if _, err := io.WriteString(gw, content); err != nil {
		return err
	}
	if err := gw.Close(); err != nil {
		return err
	}

	return os.Rename(tempDest, dest)
}

// entrySorter implements sort.Interface
type entrySorter struct {
	entries []Entry
}

func (s *entrySorter) Len() int      { return len(s.entries) }
func (s *entrySorter) Swap(i, j int) { s.entries[i], s.entries[j] = s.entries[j], s.entries[i] }
func (s *entrySorter) Less(i, j int) bool {
	ei, ej := s.entries[i], s.entries[j]
	if ei.Type() != ej.Type() {
		return ei.Type() < ej.Type() // FILE (0) < DIRECTORY (1)
	}

	hi, _ := ei.Hash() // Assuming error treated as empty or panics?
	hj, _ := ej.Hash()

	if hi != hj {
		return hi < hj
	}
	return ei.Name() < ej.Name()
}
