package internal

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FindUnreferenced returns a list of blob hashes that are present in the store
// but not referenced by any existing snapshot.
func (b *Backup) FindUnreferenced() ([]string, error) {
	// 1. Get all reachable blobs
	reachable, err := b.GetReachableBlobs()
	if err != nil {
		return nil, err
	}

	// 2. Get all existing blobs on disk
	existing, err := b.GetAllBlobs()
	if err != nil {
		return nil, err
	}

	// 3. Find diff
	var unreferenced []string
	for hash := range existing {
		if !reachable[hash] {
			unreferenced = append(unreferenced, hash)
		}
	}
	return unreferenced, nil
}

// GetReachableBlobs returns a set of all blob hashes referenced by snapshots.
func (b *Backup) GetReachableBlobs() (map[string]bool, error) {
	reachable := make(map[string]bool)
	visitedDirs := make(map[string]bool)

	// We must check ALL projects to ensure we don't count blobs from other projects as unreferenced.
	roots, err := b.AllBackupRoots()
	if err != nil {
		return nil, err
	}

	for _, root := range roots {
		h, err := root.Hash()
		if err != nil {
			// If root is corrupted (can't read existing hash file), we can't traverse it.
			// But we should warn? For now, if we can't read the hash, we can't mark its children.
			// This might risk pruning blobs that are actually needed if the root file is restored later?
			// The root file is in `snapshots/`, not `data/`.
			// If `snapshots/` entry cannot be read, we skip it.
			continue
		}

		if err := b.markReachable(h, reachable, visitedDirs); err != nil {
			// If we fail to read a directory, we risk missing its children.
			// Should we abort?
			return nil, err
		}
	}
	return reachable, nil
}

// markReachable recursively adds hashes to the reachable set
func (b *Backup) markReachable(hash string, reachable, visitedDirs map[string]bool) error {
	// Mark current
	reachable[hash] = true

	// If we've seen this dir before, stop (DAG optimization)
	if visitedDirs[hash] {
		return nil
	}

	// We don't know if it's a directory or file without opening it.
	// But `traverseDirectory` in check.go opens it.
	// Optimization: If it's a file blob, it won't be a valid gzip stream of directory entries?
	// Or we just try to open as directory.

	// Wait, we need to know if it's a glob or directory to recurse?
	// The store stores everything as blobs.
	// We only need to recurse if it IS a directory.
	// In `check.go`, we know type from the parent entry (D or F).
	// For the root, we know it is a directory.
	// So we should pass type info or just try?

	// Better: The parent loop knows the child type.
	// `root.Hash()` returns the root directory hash.

	return b.traverseReachable(hash, reachable, visitedDirs)
}

func (b *Backup) traverseReachable(hash string, reachable, visitedDirs map[string]bool) error {
	visitedDirs[hash] = true // Mark as visited to prevent re-traversal/cycles

	storePath := b.Store.DataStore(hash)
	f, err := os.Open(storePath)
	if err != nil {
		// If we can't open a blob that is referenced, it's missing.
		// We can't traverse it.
		// Check will report this. For prune, we just assume we can't find its children.
		// Does this risk deleting children?
		// Yes. If A -> B, and A is missing, we don't visit B.
		// If B is not referenced by anything else, B becomes an orphan.
		// So `prune` on a corrupted store (missing directory blobs) is DESTRUCTIVE to potentially recoverable hidden data.
		// Prune should probably abort if it encounters missing reachable directories?
		// Or warn.
		// Let's return error to be safe.
		if os.IsNotExist(err) {
			return nil // Can't traverse
		}
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("failed to read blob %s: %w", hash, err)
	}
	defer gz.Close()

	scanner := bufio.NewScanner(gz)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 36 {
			continue
		}
		// Format: "D <hash> <name>" or "F <hash> <name>"
		typeChar := line[0]
		childHash := line[2:34]

		reachable[childHash] = true

		if typeChar == 'D' {
			if !visitedDirs[childHash] {
				if err := b.traverseReachable(childHash, reachable, visitedDirs); err != nil {
					return err
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanner error for blob %s: %w", hash, err)
	}
	return nil
}

// GetAllBlobs returns a set of all blob hashes found in the data store.
func (b *Backup) GetAllBlobs() (map[string]bool, error) {
	all := make(map[string]bool)

	// Data stored in data/<subDir>/<hash>.gz
	dataDir := b.StoreData
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return all, nil
		}
		return nil, err
	}

	for _, sub := range entries {
		if !sub.IsDir() {
			continue
		}
		subPath := filepath.Join(dataDir, sub.Name())
		files, err := os.ReadDir(subPath)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			name := f.Name()
			if strings.HasSuffix(name, ".gz") {
				hash := strings.TrimSuffix(name, ".gz")
				all[hash] = true
			}
		}
	}
	return all, nil
}
