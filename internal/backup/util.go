package backup

import (
	"os"
	"path/filepath"
	"strings"
)

// ExpandPath expands tilde (~) to the user's home directory.
func ExpandPath(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	if path == "~" {
		return home, nil
	}

	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		return filepath.Join(home, path[2:]), nil
	}

	// For other tilde prefixes (e.g. ~user), we don't support them for now.
	// But let's return it as is or handle it if needed.
	return path, nil
}
