package internal

import (
	"fmt"
	"os"
)

type PruneStats struct {
	BlobsRemoved int
	BytesRemoved int64
}

// Prune deletes unreferenced blobs from the store.
func (b *Backup) Prune(dryRun bool) (PruneStats, error) {
	stats := PruneStats{}

	unreferenced, err := b.FindUnreferenced()
	if err != nil {
		return stats, err
	}

	for _, hash := range unreferenced {
		path := b.Store.DataStore(hash)

		info, err := os.Stat(path)
		if err != nil {
			// If missing, it's already gone (race or weirdness)
			if !os.IsNotExist(err) {
				// Report error but continue?
				fmt.Fprintf(os.Stderr, "Error stating to-be-pruned unreferenced blob %s: %v\n", hash, err)
			}
			continue
		}

		size := info.Size()

		if !dryRun {
			if err := os.Remove(path); err != nil {
				return stats, fmt.Errorf("failed to remove unreferenced blob %s: %w", hash, err)
			}
		}

		stats.BlobsRemoved++
		stats.BytesRemoved += size
	}

	return stats, nil
}
