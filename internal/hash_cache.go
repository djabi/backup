package internal

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

type HashCache struct {
	file  string
	top   string
	cache Properties
	dirty bool
}

func NewHashCache(top, file string) (*HashCache, error) {
	cache, err := LoadProperties(file)
	if err != nil {
		return nil, err
	}
	// Verify top path can be resolved?
	return &HashCache{
		file:  file,
		top:   top,
		cache: cache,
	}, nil
}

func (hc *HashCache) FileHash(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return "", err
	}

	relPath, err := filepath.Rel(hc.top, absPath)
	if err != nil {
		return "", fmt.Errorf("file not in backup directory: %s", path)
	}

	// Ensure we use the right separator for the key?

	// If we want to be ultra safe we can force one style, but let's stick to system default.
	key := fmt.Sprintf("%d %d %s", info.ModTime().UnixNano()/1000000, info.Size(), relPath)

	if hash, ok := hc.cache[key]; ok && hash != "" {
		return hash, nil
	}

	f, err := os.Open(absPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	hash := fmt.Sprintf("%x", h.Sum(nil))

	hc.cache[key] = hash
	hc.dirty = true

	return hash, nil
}

func (hc *HashCache) MaybeSaveCache() error {
	if !hc.dirty {
		return nil
	}
	// Custom save to enforce sort by Path (not key) to minimize diffs

	file, err := os.Create(hc.file)
	if err != nil {
		return err
	}
	defer file.Close()

	fmt.Fprintf(file, "#backup tool file hash store\n")

	type entry struct {
		key, path, val string
	}
	entries := make([]entry, 0, len(hc.cache))

	for k, v := range hc.cache {
		_, _, idx, err := parseKeyPrefix(k)
		path := ""
		if err == nil {
			path = k[idx:]
		} else {
			path = k // Fallback
		}
		entries = append(entries, entry{key: k, path: path, val: v})
	}

	// Sort by Path
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].path < entries[j].path
	})

	for _, e := range entries {
		// Escape spaces in key logic from properties.go
		// We must escape ALL spaces in the key.
		escapedKey := ""
		for _, c := range e.key {
			if c == ' ' {
				escapedKey += "\\ "
			} else {
				escapedKey += string(c)
			}
		}

		fmt.Fprintf(file, "%s=%s\n", escapedKey, e.val)
	}

	return nil
}

func (hc *HashCache) Verify() error {
	for key, hash := range hc.cache {
		// 1. Verify Hash
		if len(hash) != 32 {
			return fmt.Errorf("invalid hash length %d for key '%s'", len(hash), key)
		}
		// Check hex chars?
		for _, c := range hash {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return fmt.Errorf("invalid hash characters for key '%s': %s", key, hash)
			}
		}

		_, _, _, err := parseKeyPrefix(key)
		if err != nil {
			return fmt.Errorf("invalid cache key format: %s (%v)", key, err)
		}
	}
	return nil
}

// Prune removes entries from the cache that correspond to files that no longer exist
// or have changed (stale entries).
func (hc *HashCache) Prune() int {
	removedCount := 0
	for key := range hc.cache {
		// Key format: timestamp size path
		t, s, idx, err := parseKeyPrefix(key)
		if err != nil {
			// Malformed, remove
			delete(hc.cache, key)
			hc.dirty = true
			removedCount++
			continue
		}

		relPath := key[idx:]
		absPath := filepath.Join(hc.top, relPath)

		info, err := os.Stat(absPath)
		if os.IsNotExist(err) {
			// File gone
			delete(hc.cache, key)
			hc.dirty = true
			removedCount++
			continue
		}
		if err != nil {
			continue // Access error, keep entry? Or assume gone? Keep safe.
		}

		// Check if stale
		// Must match calculation in FileHash
		currentT := info.ModTime().UnixNano() / 1000000
		currentS := info.Size()

		if currentT != t || currentS != s {
			delete(hc.cache, key)
			hc.dirty = true
			removedCount++
		}
	}
	return removedCount
}

func parseKeyPrefix(key string) (int64, int64, int, error) {
	// timestamp size path
	var t, s int64

	idx1 := -1
	for i, c := range key {
		if c == ' ' {
			idx1 = i
			break
		}
	}
	if idx1 <= 0 {
		return 0, 0, 0, fmt.Errorf("missing timestamp delimiter")
	}

	idx2 := -1
	for i := idx1 + 1; i < len(key); i++ {
		if key[i] == ' ' {
			idx2 = i
			break
		}
	}
	if idx2 <= idx1+1 || idx2 >= len(key)-1 {
		return 0, 0, 0, fmt.Errorf("missing size delimiter or path")
	}

	// Parse first two fields
	if _, err := fmt.Sscanf(key[:idx2], "%d %d", &t, &s); err != nil {
		return 0, 0, 0, err
	}

	return t, s, idx2 + 1, nil
}
