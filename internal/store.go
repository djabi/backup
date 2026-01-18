package internal

import (
	"compress/gzip"
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Store struct {
	b *Backup
}

func NewStore(b *Backup) *Store {
	return &Store{b: b}
}

// DataStore returns the path to the stored file for a given hash.
func (s *Store) DataStore(hash string) string {
	if len(hash) < 2 {
		return ""
	}
	subStore := hash[:2]
	return filepath.Join(s.b.StoreData, subStore, hash+".gz")
}

// Copy copies from in to out using a buffer.
func Copy(in io.Reader, out io.Writer) error {
	_, err := io.Copy(out, in)
	return err
}

// GzipContentHash calculates the MD5 of the uncompressed content of a gzip file.
func (s *Store) GzipContentHash(gzipPath string) (string, error) {
	f, err := os.Open(gzipPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, gz); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// CleanupPartials removes any leftover .partial files in the store.
// Returns the number of files removed.
func (s *Store) CleanupPartials() (int, error) {
	count := 0
	err := filepath.Walk(s.b.StoreData, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err // Or return nil to continue? Better to report.
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".partial") {
			if s.b.DryRun {
				fmt.Printf("[dry-run] Would remove partial file: %s\n", path)
				count++
			} else {
				if err := os.Remove(path); err != nil {
					// Warn but continue
					fmt.Fprintf(os.Stderr, "Warning: failed to remove partial file %s: %v\n", path, err)
				} else {
					count++
				}
			}
		}
		return nil
	})
	return count, err
}
