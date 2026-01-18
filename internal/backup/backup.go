package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type Backup struct {
	Top               string
	CurrentWorkingDir string
	BackupConfigDir   string
	StoreRoot         string
	ProjectName       string
	StoreData         string
	StoreSnapshots    string
	Config            *Config
	Store             *Store
	HashCache         *HashCache
	DryRun            bool
	Stats             BackupStats
}

type BackupStats struct {
	FilesTotal    int
	FilesArchived int
	FilesIgnored  int
	DirsTotal     int
	DirsArchived  int
	DirsIgnored   int
	BytesArchived int64
	BytesTotal    int64
}

func NewBackup(startDir, storeDir string, assumeYes bool) (*Backup, error) {
	b := &Backup{}
	var err error

	// 1. Determine StoreRoot if provided explicitly
	if storeDir != "" {
		expanded, err := ExpandPath(storeDir)
		if err != nil {
			return nil, err
		}
		b.StoreRoot, err = filepath.Abs(expanded)
		if err != nil {
			return nil, err
		}
	}

	// 2. Determine potential source directory
	var cwd string
	if startDir != "" {
		cwd, err = filepath.Abs(startDir)
		if err != nil {
			return nil, err
		}
	} else {
		cwd, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}

	// 3. Look for .backup configuration in tree
	top := lookupTop(cwd)
	if top != "" {
		// Check for store.toml (Store Mode)
		storeConfigPath := filepath.Join(top, ".backup", "store.toml")
		if _, err := os.Stat(storeConfigPath); err == nil {
			// Found store.toml -> This is a backup store
			b.StoreRoot = top
			// Ensure we treat this as headless/store mode (Empty Top) so restore requires destination
			b.Top = ""
		} else {
			// Check for config.toml (Source Mode)
			configPath := filepath.Join(top, ".backup", "config.toml")
			if _, err := os.Stat(configPath); err == nil {
				// Found config.toml -> This is a source directory
				b.CurrentWorkingDir = cwd
				b.Top = top
				b.BackupConfigDir = filepath.Join(top, ".backup")

				// Load config
				b.Config, err = LoadConfig(configPath)
				if err != nil {
					return nil, fmt.Errorf("failed to load config from %s: %v", configPath, err)
				}

				// If store not explicitly provided, look in config
				if b.StoreRoot == "" {
					backupStoreSetting := b.Config.Store
					if backupStoreSetting != "" {
						expanded, err := ExpandPath(backupStoreSetting)
						if err != nil {
							return nil, err
						}
						if filepath.IsAbs(expanded) {
							b.StoreRoot = expanded
						} else {
							b.StoreRoot = filepath.Join(top, expanded)
						}
						// Canonize path
						b.StoreRoot, err = filepath.Abs(b.StoreRoot)
						if err != nil {
							return nil, err
						}
					}
				}

				// Determine ProjectName
				if b.Config.Name != "" {
					b.ProjectName = b.Config.Name
				}
			}
		}
	}

	// 4. Auto-detect store if not specified (Legacy detection or implicit current dir)
	if b.StoreRoot == "" {
		// Check if current directory looks like a store (data/ and snapshots/ exist)
		// This is a fallback if store.toml is missing but structure matches
		dataDir := filepath.Join(cwd, "data")
		backupsDir := filepath.Join(cwd, "snapshots")

		isStore := false
		if info, err := os.Stat(dataDir); err == nil && info.IsDir() {
			if info, err := os.Stat(backupsDir); err == nil && info.IsDir() {
				isStore = true
			}
		}

		if isStore {
			b.StoreRoot = cwd
		}
	}

	// 5. Validation
	if b.StoreRoot == "" {
		return nil, fmt.Errorf("no backup configuration found\n\n" +
			"To get started:\n" +
			"  • Initialize a new backup store:  backup init-store <path>\n" +
			"  • Initialize a source directory:  backup init <path>\n" +
			"  • Specify store explicitly:       backup --store <path> <command>\n\n" +
			"Run 'backup --help' for more information.")
	}

	info, err := os.Stat(b.StoreRoot)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("backup store is not a directory: %s", b.StoreRoot)
	}

	// 6. Initialize Store structure
	b.StoreData = filepath.Join(b.StoreRoot, "data")
	if err := os.MkdirAll(b.StoreData, 0755); err != nil {
		return nil, err
	}

	b.StoreSnapshots = filepath.Join(b.StoreRoot, "snapshots")
	if err := os.MkdirAll(b.StoreSnapshots, 0755); err != nil {
		return nil, err
	}

	// Ensure .backup directory exists in store
	storeBackupDir := filepath.Join(b.StoreRoot, ".backup")
	if err := os.MkdirAll(storeBackupDir, 0755); err != nil {
		return nil, err
	}

	// Ensure store.toml exists
	storeTomlPath := filepath.Join(storeBackupDir, "store.toml")
	if _, err := os.Stat(storeTomlPath); os.IsNotExist(err) {
		// Prompt for confirmation if not assumeYes
		if !assumeYes {
			// Check if interactive
			fileInfo, _ := os.Stdin.Stat()
			if (fileInfo.Mode() & os.ModeCharDevice) == 0 {
				return nil, fmt.Errorf("store configuration missing in %s and running non-interactively; use --yes to create", b.StoreRoot)
			}

			fmt.Printf("Store configuration missing in %s. Create store.toml? [y/N] ", b.StoreRoot)
			var response string
			fmt.Scanln(&response) // Simple scan
			if response != "y" && response != "Y" && response != "yes" {
				return nil, fmt.Errorf("store initialization aborted by user")
			}
		}

		if err := os.WriteFile(storeTomlPath, []byte("store = \".\"\n"), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to create store.toml: %v\n", err)
		}
	}

	// Hash cache logic needs Top?
	// If Top is missing (store-only mode), we might not have a place for hash-cache or config-based hash-cache.
	// For now, only initialize HashCache if Top is present.
	if b.Top != "" {
		b.HashCache, err = NewHashCache(b.Top, filepath.Join(b.BackupConfigDir, "hash-cache"))
		if err != nil {
			return nil, err
		}
	}

	b.Store = NewStore(b)

	return b, nil
}

func (b *Backup) BackupRoots() ([]*BackupRoot, error) {
	var roots []*BackupRoot

	searchDir := b.StoreSnapshots
	if b.ProjectName != "" {
		searchDir = filepath.Join(b.StoreSnapshots, b.ProjectName)
		if _, err := os.Stat(searchDir); os.IsNotExist(err) {
			return []*BackupRoot{}, nil
		}
		// List files in specific project dir
		files, err := os.ReadDir(searchDir)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			root, err := NewBackupRoot(b, filepath.Join(searchDir, f.Name()))
			if err != nil { // Skip invalid
				continue
			}
			roots = append(roots, root)
		}
	} else {
		// ProjectName empty -> Search all subdirectories
		params, err := os.ReadDir(searchDir)
		if err == nil {
			for _, p := range params {
				if p.IsDir() {
					projectDir := filepath.Join(searchDir, p.Name())
					files, err := os.ReadDir(projectDir)
					if err == nil {
						for _, f := range files {
							if f.IsDir() {
								continue
							}
							root, err := NewBackupRoot(b, filepath.Join(projectDir, f.Name()))
							if err == nil {
								roots = append(roots, root)
							}
						}
					}
				}
			}
		}
	}
	sort.Sort(BackupRoots(roots))
	return roots, nil
}

// AllBackupRoots returns all backup roots from all projects in the store,
// ignoring the current project context.
func (b *Backup) AllBackupRoots() ([]*BackupRoot, error) {
	var roots []*BackupRoot
	searchDir := b.StoreSnapshots

	// Search all subdirectories
	params, err := os.ReadDir(searchDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*BackupRoot{}, nil
		}
		return nil, err
	}

	for _, p := range params {
		if p.IsDir() {
			projectDir := filepath.Join(searchDir, p.Name())
			files, err := os.ReadDir(projectDir)
			if err == nil {
				for _, f := range files {
					if f.IsDir() {
						continue
					}
					root, err := NewBackupRoot(b, filepath.Join(projectDir, f.Name()))
					if err == nil {
						roots = append(roots, root)
					}
				}
			}
		}
	}
	sort.Sort(BackupRoots(roots))
	return roots, nil
}

func (b *Backup) LatestBackupRoot() (*BackupRoot, error) {
	roots, err := b.BackupRoots()
	if err != nil {
		return nil, err
	}
	if len(roots) == 0 {
		return nil, nil
	}
	return roots[len(roots)-1], nil
}

func (b *Backup) FindBackupRoot(name string) (*BackupRoot, error) {
	path := ""
	// If name contains separators, assume it's relative path from snapshots root (e.g "proj/timestamp")
	// Or absolute path? Let's check if it exists relative to StoreSnapshots first if "clean".

	// If project name is set and name is just timestamp
	if b.ProjectName != "" && filepath.Base(name) == name {
		path = filepath.Join(b.StoreSnapshots, b.ProjectName, name)
	} else {
		// Try direct from snapshots root
		path = filepath.Join(b.StoreSnapshots, name)
	}

	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	return NewBackupRoot(b, path)
}

func (b *Backup) BackupDirectory(hash, name string) *BackupDirectory {

	// For now new instance is fine as long as it's stateless representation of that hash.
	// The content loading is inside BackupDirectory.
	return NewBackupDirectory(b, hash, name)
}

func lookupTop(current string) string {
	for current != "/" && current != "." {
		backupDir := filepath.Join(current, ".backup")
		if info, err := os.Stat(backupDir); err == nil && info.IsDir() {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return ""
}
