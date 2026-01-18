package internal

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type BackupEntry interface {
	Hash() string
	Name() string
	Restore(dest string) error
}

type BaseBackupEntry struct {
	b    *Backup
	hash string
	name string
}

func (e *BaseBackupEntry) Hash() string { return e.hash }
func (e *BaseBackupEntry) Name() string { return e.name }

func (e *BaseBackupEntry) Restore(dest string) error {
	return fmt.Errorf("not implemented")
}

type BackupFile struct {
	BaseBackupEntry
}

func NewBackupFile(b *Backup, hash, name string) *BackupFile {
	return &BackupFile{BaseBackupEntry{b: b, hash: hash, name: name}}
}

func (f *BackupFile) Restore(dest string) error {
	storePath := f.b.Store.DataStore(f.hash)
	src, err := os.Open(storePath)
	if err != nil {
		return fmt.Errorf("failed to open store file: %w", err)
	}
	defer src.Close()

	gz, err := gzip.NewReader(src)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gz.Close()

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("failed to create destination dir: %w", err)
	}

	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, gz); err != nil {
		return fmt.Errorf("failed to copy content: %w", err)
	}

	return nil
}

type BackupLink struct {
	BaseBackupEntry
}

func NewBackupLink(b *Backup, hash, name string) *BackupLink {
	return &BackupLink{BaseBackupEntry{b: b, hash: hash, name: name}}
}

func (l *BackupLink) Restore(dest string) error {
	storePath := l.b.Store.DataStore(l.hash)
	src, err := os.Open(storePath)
	if err != nil {
		return fmt.Errorf("failed to open store file: %w", err)
	}
	defer src.Close()

	gz, err := gzip.NewReader(src)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gz.Close()

	// Read target path
	content, err := io.ReadAll(gz)
	if err != nil {
		return fmt.Errorf("failed to read link target: %w", err)
	}
	target := string(content)

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("failed to create destination dir: %w", err)
	}

	// Remove existing if any
	if _, err := os.Lstat(dest); err == nil {
		if err := os.Remove(dest); err != nil {
			return fmt.Errorf("failed to remove existing file: %w", err)
		}
	}

	if err := os.Symlink(target, dest); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}

	return nil
}

type BackupDirectory struct {
	BaseBackupEntry
	entries map[string]BackupEntry
}

func NewBackupDirectory(b *Backup, hash, name string) *BackupDirectory {
	return &BackupDirectory{BaseBackupEntry: BaseBackupEntry{b: b, hash: hash, name: name}}
}

func (d *BackupDirectory) Restore(dest string) error {
	entries, err := d.Entries()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dest, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dest, err)
	}

	for name, entry := range entries {
		childDest := filepath.Join(dest, name)
		if err := entry.Restore(childDest); err != nil {
			return err
		}
	}
	return nil
}

func (d *BackupDirectory) Entries() (map[string]BackupEntry, error) {
	if d.entries != nil {
		return d.entries, nil
	}

	d.entries = make(map[string]BackupEntry)

	// Read GZiped content
	storePath := d.b.Store.DataStore(d.hash)
	f, err := os.Open(storePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open store file %s: %v", storePath, err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	scanner := bufio.NewScanner(gz)
	for scanner.Scan() {
		line := scanner.Text()
		// Format: T hash name
		// T is 1 char, then space (index 1), hash is 32 chars (index 2-34), space (index 34), name (index 35+)
		if len(line) < 36 || line[1] != ' ' || line[34] != ' ' {
			fmt.Fprintf(os.Stderr, "Warning: invalid directory entry: %s\n", line)
			continue
		}

		typeChar := line[0]
		hash := line[2:34]
		name := line[35:]

		switch typeChar {
		case 'D':
			d.entries[name] = NewBackupDirectory(d.b, hash, name)
		case 'F':
			d.entries[name] = NewBackupFile(d.b, hash, name)
		case 'L':
			d.entries[name] = NewBackupLink(d.b, hash, name)
		default:
			fmt.Fprintf(os.Stderr, "Warning: unknown entry type: %c\n", typeChar)
		}
	}

	return d.entries, scanner.Err()
}
