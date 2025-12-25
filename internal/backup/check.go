package backup

import (
	"bufio"
	"compress/gzip"
	"crypto/md5"
	"fmt"
	"io"
	"os"
)

// Verify checks the integrity of the backup store.
// If deep is true, it verifies the content hash of every blob.
// It returns a list of errors found (missing files, corrupted content).
func (b *Backup) Verify(deep bool) []error {
	var errs []error
	verifiedBlobs := make(map[string]bool)
	traversedDirs := make(map[string]bool)

	roots, err := b.BackupRoots()
	if err != nil {
		return []error{fmt.Errorf("failed to list backup roots: %w", err)}
	}

	for _, root := range roots {
		// Verify root blob exists
		h, err := root.Hash()
		if err != nil {
			errs = append(errs, fmt.Errorf("root %s corrupted: %w", root.BackupHead, err))
			continue
		}

		// Traverse
		if err := b.verifyTree(h, deep, verifiedBlobs, traversedDirs, &errs); err != nil {
			errs = append(errs, fmt.Errorf("traversal error for root %s: %w", root.BackupHead, err))
		}
	}

	// Unreferenced blobs
	unreferenced, err := b.FindUnreferenced()
	if err != nil {
		errs = append(errs, fmt.Errorf("unreferenced blob detection failed: %w", err))
	} else if len(unreferenced) > 0 {
		// Report unreferenced blobs as errors?
		// The user request was "detection of orphaned blobs in the check command".
		// Typically orphans are not "errors" in integrity, but they are "cleanliness" issues.
		// `git fsck` reports them.
		// "integrity check failed" output in CLI implies CRITICAL errors.
		// If I return them as errors, command exits with 1.
		// Ideally we report them as warnings or just listed.
		// But `Verify` signature is `[]error`.
		// Let's create a custom error type or just format it.
		// "Unreferenced blob: <hash>"
		for _, o := range unreferenced {
			errs = append(errs, fmt.Errorf("unreferenced blob: %s", o))
		}
	}

	// Check hash cache if present
	if b.HashCache != nil {
		if err := b.HashCache.Verify(); err != nil {
			errs = append(errs, fmt.Errorf("hash cache verification failed: %w", err))
		}
	}

	return errs
}

func (b *Backup) verifyTree(hash string, deep bool, verifiedBlobs, traversedDirs map[string]bool, errs *[]error) error {
	// Root is a directory, so we verify blob and traverse
	if err := b.verifyBlob(hash, deep, verifiedBlobs, errs); err != nil {
		return err // Blob invalid
	}
	return b.traverseDirectory(hash, deep, verifiedBlobs, traversedDirs, errs)
}

func (b *Backup) verifyBlob(hash string, deep bool, verifiedBlobs map[string]bool, errs *[]error) error {
	if verifiedBlobs[hash] {
		return nil
	}

	storePath := b.Store.DataStore(hash)

	// 1. Check existence
	info, err := os.Stat(storePath)
	if os.IsNotExist(err) {
		*errs = append(*errs, fmt.Errorf("missing blob: %s (path: %s)", hash, storePath))
		verifiedBlobs[hash] = true // Mark as visited to avoid repeated error
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		*errs = append(*errs, fmt.Errorf("empty blob: %s", hash))
		verifiedBlobs[hash] = true
		return nil
	}

	// 2. Check content integrity (Deep)
	if deep {
		if err := verifyBlobHash(storePath, hash); err != nil {
			*errs = append(*errs, fmt.Errorf("corrupted blob %s: %w", hash, err))
			verifiedBlobs[hash] = true
			return nil
		}
	}

	verifiedBlobs[hash] = true
	return nil
}

func (b *Backup) traverseDirectory(hash string, deep bool, verifiedBlobs, traversedDirs map[string]bool, errs *[]error) error {
	if traversedDirs[hash] {
		return nil
	}
	traversedDirs[hash] = true

	storePath := b.Store.DataStore(hash)
	f, err := os.Open(storePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("failed to read dir content %s: %w", hash, err))
		return nil
	}
	defer gz.Close()

	scanner := bufio.NewScanner(gz)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 36 {
			continue
		}
		typeChar := line[0]
		childHash := line[2:34]

		// Always verify the child blob exists/is valid
		// This handles files and directories blobs.
		b.verifyBlob(childHash, deep, verifiedBlobs, errs)

		// If directory, recurse too
		if typeChar == 'D' {
			if err := b.traverseDirectory(childHash, deep, verifiedBlobs, traversedDirs, errs); err != nil {
				// Don't append error here, assume traverseDirectory appended specifics
			}
		}
	}
	return nil
}

func verifyBlobHash(path, expectedHash string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip error: %w", err)
	}
	defer gz.Close()

	h := md5.New()
	if _, err := io.Copy(h, gz); err != nil {
		return fmt.Errorf("hashing error: %w", err)
	}

	actualHash := fmt.Sprintf("%x", h.Sum(nil))
	if actualHash != expectedHash {
		return fmt.Errorf("hash mismatch: expected %s, got %s", expectedHash, actualHash)
	}
	return nil
}
