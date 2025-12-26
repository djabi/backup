package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"djabi.dev/go/backup/internal/backup"
	"github.com/urfave/cli/v2"
)

func main() {
	var b *backup.Backup

	app := &cli.App{
		Name:    "backup",
		Usage:   "Content-addressable backup tool with deduplication, incremental backups, and integrity verification",
		Version: "1.0.0",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "root",
				Aliases: []string{"d"},
				Usage:   "Root directory of the backup (optional)",
			},
			&cli.StringFlag{
				Name:    "store",
				Aliases: []string{"s"},
				Usage:   "Backup store directory (optional)",
			},
			&cli.BoolFlag{
				Name:    "yes",
				Aliases: []string{"y"},
				Usage:   "Automatically answer yes to prompts (e.g. store creation)",
			},
		},
		Before: func(c *cli.Context) error {
			cmdName := c.Args().First()
			if cmdName == "init" || cmdName == "init-store" || cmdName == "help" || cmdName == "h" || cmdName == "version" || c.Bool("version") {
				return nil
			}
			var err error
			root := c.String("root")
			store := c.String("store")
			assumeYes := c.Bool("yes")
			b, err = backup.NewBackup(root, store, assumeYes)
			if err != nil {
				return fmt.Errorf("error initializing backup: %w", err)
			}
			return nil
		},
		Commands: []*cli.Command{
			{
				Name:    "version",
				Usage:   "Print the version",
				Aliases: []string{"v"},
				Action: func(c *cli.Context) error {
					fmt.Printf("backup-cli version %s\n", c.App.Version)
					return nil
				},
			},
			{
				Name:      "init-store",
				Usage:     "Initialize a new backup store",
				ArgsUsage: "[path]",
				Action: func(c *cli.Context) error {
					path := c.Args().First()
					if path == "" {
						path = "."
					}
					return runInitStore(path)
				},
			},
			{
				Name:      "init",
				Usage:     "Initialize a new source directory",
				ArgsUsage: "[path]",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "store",
						Usage: "Path to backup store",
					},
					&cli.StringFlag{
						Name:  "project",
						Usage: "Project name",
					},
				},
				Action: func(c *cli.Context) error {
					path := c.Args().First()
					if path == "" {
						path = "."
					}
					store := c.String("store")
					project := c.String("project")
					return runInit(path, store, project)
				},
			},
			{
				Name:  "backup",
				Usage: "Create a new backup",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "dry-run",
						Usage: "Perform a dry run without writing changes",
					},
				},
				Action: func(c *cli.Context) error {
					b.DryRun = c.Bool("dry-run")
					return runBackup(b)
				},
			},
			{
				Name:    "snapshots",
				Aliases: []string{"snapshot", "list"},
				Usage:   "List backup snapshots",
				Action: func(c *cli.Context) error {
					return runSnapshots(b)
				},
			},
			{
				Name:  "tree",
				Usage: "List contents of a backup",
				Action: func(c *cli.Context) error {
					arg := c.Args().First()
					return runTree(b, arg)
				},
			},
			{
				Name:  "status",
				Usage: "Show status",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name: "show-ignored",
					},
				},
				Action: func(c *cli.Context) error {
					return b.Status(c.Bool("show-ignored"))
				},
			},
			{
				Name:  "check",
				Usage: "Check the integrity of the backup store",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "deep",
						Usage: "Verify content hashes (slow)",
					},
				},
				Action: func(c *cli.Context) error {
					deep := c.Bool("deep")
					fmt.Printf("Checking store integrity (deep=%v)...\n", deep)
					errs := b.Verify(deep)
					if len(errs) > 0 {
						fmt.Println("Integrity check failed with errors:")
						for _, e := range errs {
							fmt.Printf(" - %v\n", e)
						}
						return fmt.Errorf("store integrity check failed")
					}
					fmt.Println("Store integrity check passed.")
					return nil
				},
			},
			{
				Name:  "prune",
				Usage: "Remove unused blobs from the store",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "dry-run",
						Usage: "Do not delete files, only show what would be deleted",
					},
				},
				Action: func(c *cli.Context) error {
					dryRun := c.Bool("dry-run")
					stats, err := b.Prune(dryRun)
					if err != nil {
						return fmt.Errorf("prune failed: %w", err)
					}
					if dryRun {
						fmt.Printf("[dry-run] Found %d unreferenced blobs, would reclaim %d bytes\n", stats.BlobsRemoved, stats.BytesRemoved)
					} else {
						fmt.Printf("Pruned %d unreferenced blobs, reclaimed %d bytes\n", stats.BlobsRemoved, stats.BytesRemoved)
					}
					return nil
				},
			},
			{
				Name:      "remove",
				Aliases:   []string{"rm", "forget", "delete"},
				Usage:     "Remove one or more backup snapshots",
				ArgsUsage: "<snapshot> [snapshot...]",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "dry-run",
						Usage: "Show what would be deleted without actually removing anything",
					},
				},
				Action: func(c *cli.Context) error {
					snapshots := c.Args().Slice()
					if len(snapshots) == 0 {
						return fmt.Errorf("at least one snapshot ID is required")
					}
					b.DryRun = c.Bool("dry-run")
					return runRemove(b, snapshots)
				},
			},
			{
				Name:  "prune-cache",
				Usage: "Prune entries from the hash cache for missing files",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "dry-run",
						Usage: "Show what would be removed without actually removing anything",
					},
				},
				Action: func(c *cli.Context) error {
					if b.HashCache == nil {
						return fmt.Errorf("prune-cache requires running from a source directory with hash-cache enabled")
					}
					dryRun := c.Bool("dry-run")
					return runPruneCache(b, dryRun)
				},
			},
			{
				Name:      "restore",
				Usage:     "Restore from a backup snapshot",
				ArgsUsage: "<snapshot> [path] [destination]",
				Description: "Restore a snapshot or a path within a snapshot.\n" +
					"   If running from source directory, destination defaults to current directory.\n" +
					"   Arguments:\n" +
					"     <snapshot>     Timestamp or project/timestamp of the backup.\n" +
					"     [path]         (Optional) Path of file/dir inside the backup to restore.\n" +
					"     [destination]  (Optional) Destination path to restore to.",
				Action: func(c *cli.Context) error {
					args := c.Args()
					if args.Len() < 1 {
						return fmt.Errorf("snapshot name required")
					}
					snapshotName := args.Get(0)

					// Parse optional args
					var pathInside, dest string

					if args.Len() == 1 {
						// restore <snapshot> -> restore root to context default or error
						pathInside = ""
						dest = ""
					} else if args.Len() == 2 {
						// restore <snapshot> <dest> OR restore <snapshot> <path> ?
						// Ambiguous. Usually implicit destination implies the LAST arg is missing.
						// If we want to support "restore <snapshot> <path>", we need to know where to restore it.
						// Strategy:
						// If 2 args: assume <snapshot> <dest> (restoring root to dest)
						// OR <snapshot> <path_inside> (restoring path to default dest)?
						// Let's look at user request: "restore to provide a destination... one doesn't need to provide destination directory".
						// If user types `restore <snap> <arg>`, is <arg> the source path or destination?
						// CLI convention: `cp <src> <dest>`.
						// If we treat `restore <snap>` as "cp <snap> .", then `restore <snap> <foo>` is "cp <snap>/foo ." or "cp <snap> foo"?
						// Standard `tar -xf archive path` extracts path to current dir.
						// `tar -xf archive -C dest`.
						// Let's assume positional args: <snapshot> [path_inside_snapshot] [destination_on_disk].
						// If 1 arg: <snapshot> -> restore root to default.
						// If 2 args: <snapshot> <path_inside> -> restore path to default.
						// If 3 args: <snapshot> <path_inside> <dest>.
						// BUT user said "require restore to provide a destination".
						// So if strictly headless: `restore <snap> <dest>` (restoring root).
						// Maybe we need flags or heuristic.
						// Heuristic:
						// 1. If b.Top is set (source context), default dest is CWD.
						//    Then args are likely <snapshot> [path].
						// 2. If b.Top is NOT set (headless), dest is required.
						//    Then args: <snapshot> <dest> (restoring root) OR <snapshot> <path> <dest>.
						//    This is ambiguous if 2 args.

						// Let's stick to simple flexible parsing?
						// Let's assume the user meant:
						// If in source dir: `restore <snap>` (restore all), `restore <snap> <file>` (restore file).
						// If NOT in source dir: `restore <snap> <dest>` (restore all to dest), `restore <snap> <file> <dest>` (restore file to dest).

						if b.Top != "" {
							// Source context
							pathInside = args.Get(1)
							dest = "" // triggers default logic
						} else {
							// Headless context
							// Support restoring root only? Or detecting if arg 1 looks like a path in backup?
							// Safest: assume 2nd arg is destination if only 2 args and no context?
							// Or assume 2nd arg is path inside, and we need 3rd arg for dest?
							// User prompt: "when... run from inside a <store> directory, it understands that and requires restore to privide a destination"
							// So `restore <snap>` fails. `restore <snap> <dest>` works.
							dest = args.Get(1)
							pathInside = ""
						}
					} else if args.Len() >= 3 {
						pathInside = args.Get(1)
						dest = args.Get(2)
					}

					return runRestore(b, snapshotName, pathInside, dest)
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func runSnapshots(b *backup.Backup) error {
	roots, err := b.BackupRoots()
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}

	for _, root := range roots {
		h, err := root.Hash()
		if err != nil {
			fmt.Printf("%s <error: %v>\n", root, err)
			continue
		}
		fmt.Printf("%s %s\n", root, h)
	}
	fmt.Printf("%d snapshots found\n", len(roots))
	return nil
}

func runTree(b *backup.Backup, rootName string) error {
	var root *backup.BackupRoot
	var err error

	if rootName == "" {
		root, err = b.LatestBackupRoot()
		if err != nil {
			return err
		}
		if root == nil {
			fmt.Println("No backups found.")
			return nil
		}
	} else {
		root, err = b.FindBackupRoot(rootName)
		if err != nil {
			return fmt.Errorf("backup root not found: %s", rootName)
		}
	}

	top, err := root.TopDirectory()
	if err != nil {
		return err
	}

	// Just print content text for now (not recursive tree yet, unless requested?)
	// User request: "list the file in a give backup root"

	// but the description said "Lists the backup root timestamps".
	// The user now asks for "list the file".
	// Let's print the directory content (one level or recursive?).
	// "list the file" (singular) might mean listing ALL files (recursive) or just one level.
	// `tree` usually implies recursive.
	// Let's implement recursive tree printer.

	fmt.Printf("Listing content for backup %s\n", root)
	return printTree(top, "")
}

func printTree(dir *backup.BackupDirectory, prefix string) error {
	entries, err := dir.Entries()
	if err != nil {
		return err
	}

	// Sort entries by name for consistent output
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		entry := entries[name]
		// D or F ?
		// We can check type assertions
		if d, ok := entry.(*backup.BackupDirectory); ok {
			fmt.Printf("%s%s/ (%s)\n", prefix, name, d.Hash()[:7]) // Short hash
			if err := printTree(d, prefix+"  "); err != nil {
				return err
			}
		} else if f, ok := entry.(*backup.BackupFile); ok {
			fmt.Printf("%s%s (%s)\n", prefix, name, f.Hash()[:7])
		}
	}
	return nil
}

func runBackup(b *backup.Backup) error {
	if b.Top == "" {
		msg := "Run 'backup' from a source directory. Current directory is not initialized."
		if b.StoreRoot != "" {
			msg += fmt.Sprintf("\nIt looks like you are running inside a store directory: %s", b.StoreRoot)
		}
		return fmt.Errorf("%s", msg)
	}

	// Ensure READMEs exist (auto-fix for existing setups)
	if err := ensureSourceReadme(b.BackupConfigDir); err != nil {
		// Non-fatal warning
		fmt.Fprintf(os.Stderr, "Warning: Failed to create source README: %v\n", err)
	}
	if b.StoreRoot != "" {
		if err := ensureStoreReadme(b.StoreRoot); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to create store README: %v\n", err)
		}
	}

	fmt.Println("Starting backup...")
	if b.DryRun {
		fmt.Println("Running in dry-run mode")
	}

	// Reset stats
	b.Stats = backup.BackupStats{}

	top := backup.NewDirectoryEntry(b, b.Top, nil)

	if err := top.Save(); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	if b.DryRun {
		fmt.Println("[dry-run] Would write backup head")
		fmt.Println("[dry-run] Would save hash cache")
	} else {
		// Write backup head
		h, err := top.Hash()
		if err != nil {
			return fmt.Errorf("failed to calculate top hash: %w", err)
		}

		var headDir string
		if b.ProjectName != "" {
			headDir = filepath.Join(b.StoreSnapshots, b.ProjectName)
		} else {
			headDir = b.StoreSnapshots
		}

		if err := os.MkdirAll(headDir, 0755); err != nil {
			return fmt.Errorf("failed to create snapshot dir %s: %w", headDir, err)
		}

		// Format: yyMMdd-HHmmss
		var timestamp string
		var headFile string
		for {
			timestamp = time.Now().Format("060102-150405")
			headFile = filepath.Join(headDir, timestamp)
			if _, err := os.Stat(headFile); os.IsNotExist(err) {
				break
			}
			// Collision, wait enabling unique timestamp (1s resolution)
			time.Sleep(100 * time.Millisecond)
		}

		if err := os.WriteFile(headFile, []byte(h+"\n"), 0644); err != nil {
			return fmt.Errorf("failed to write backup head: %w", err)
		}

		// Prune cache for missing files before saving
		if b.HashCache != nil {
			pruned := b.HashCache.Prune()
			if pruned > 0 {
				if b.Stats.FilesArchived > 0 { // Just verbose logging if needed, or silent?
					// Standard output for backup usually summarizes file ops.
					// Maybe just log if we want to be chatty, but "Pruned x entries" might be noisy.
					// Let's keep it silent unless it's a dedicated command, as requested,
					// "No point of keeping thouse" implies automatic cleanup.
				}
			}
		}

		// Save cache
		if err := b.HashCache.MaybeSaveCache(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to save hash cache: %v\n", err)
		}

		msg := fmt.Sprintf("Backup completed successfully. Head: %s", timestamp)
		if b.ProjectName != "" {
			msg += fmt.Sprintf(" (Project: %s)", b.ProjectName)
		}
		fmt.Println(msg)
	}

	fmt.Println("\nBackup Summary:")
	fmt.Printf("  Files:       %d total, %d archived\n", b.Stats.FilesTotal, b.Stats.FilesArchived)
	fmt.Printf("  Directories: %d total, %d archived\n", b.Stats.DirsTotal, b.Stats.DirsArchived)
	fmt.Printf("  Bytes:       %d archived\n", b.Stats.BytesArchived)

	return nil
}

func runRestore(b *backup.Backup, snapshotName, pathInside, dest string) error {
	// 1. Locate backup root
	var root *backup.BackupRoot
	var err error

	root, err = b.FindBackupRoot(snapshotName)
	if err != nil {
		return fmt.Errorf("snapshot not found: %s", snapshotName)
	}

	// 2. Locate entry to restore
	// Resolve pathInside if in source context and it's relative
	resolvedPathInside := pathInside
	if b.Top != "" && pathInside != "" && !filepath.IsAbs(pathInside) {
		// If pathInside is "sub/file.txt" and we are in "sub", user might mean "sub/sub/file.txt" (standard)
		// OR "sub/file.txt" relative to root?
		// Standard unix tools (tar, git) use path relative to CWD if implied.
		// git checkout file.txt -> file.txt in CWD.
		// so if CWD is "sub", looking for "sub/file.txt" (relative to root).
		// We need to convert CWD-relative path to Root-relative path to find it in snapshot.

		relCwd, err := filepath.Rel(b.Top, b.CurrentWorkingDir)
		if err == nil && relCwd != "." {
			resolvedPathInside = filepath.Join(relCwd, pathInside)
		}
	}

	entry, err := root.Locate(resolvedPathInside)
	if err != nil {
		return fmt.Errorf("failed to locate path '%s' (resolved: '%s') in snapshot: %w", pathInside, resolvedPathInside, err)
	}
	if entry == nil {
		// Try original path logic?
		// If user typed "sub/file.txt" from "sub" but meant root? Rare.
		// Fallback? No, strict is better.
		return fmt.Errorf("path '%s' not found in snapshot %s", resolvedPathInside, snapshotName)
	}

	// 3. Determine destination
	if dest == "" {
		if b.Top != "" {
			// Context: Source directory
			if pathInside == "" {
				dest = "." // restore root to current dir (or root? CWD is safer default)
			} else {
				// restoring a file/dir, default to ./<name>
				dest = entry.Name()
				// Use base name of what user typed?
				// If user typed "file.txt", we restore to "file.txt" (in CWD).
				// dest is relative to CWD.
				// entry.Name() is just the name.
				// So if we are in CWD and write "file.txt", it goes to CWD/file.txt. Correct.
			}
		} else {
			return fmt.Errorf("destination required when not running from source directory")
		}
	}

	fmt.Printf("Restoring %s from %s to %s...\n", pathInside, snapshotName, dest)
	if b.DryRun {
		fmt.Println("[dry-run] Would restore content")
		return nil
	}

	if err := entry.Restore(dest); err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	fmt.Println("Restore complete.")
	return nil
}

func runRemove(b *backup.Backup, snapshots []string) error {
	for _, name := range snapshots {
		// Verify existence
		root, err := b.FindBackupRoot(name)
		if err != nil {
			fmt.Printf("Error: Snapshot '%s' not found or invalid: %v\n", name, err)
			continue
		}

		if b.DryRun {
			fmt.Printf("[dry-run] Would remove snapshot %s\n", root)
			continue
		}

		fmt.Printf("Removing snapshot %s...\n", root)
		if err := os.Remove(root.BackupHead); err != nil {
			fmt.Printf("Error: Failed to remove snapshot file %s: %v\n", root.BackupHead, err)
			continue
		}
		// Optional: Clean up project directory if empty?
		// We can leave it for now.
	}

	if b.DryRun {
		fmt.Println("[dry-run] Would prune unreferenced data blobs")
		// We could run prune --dry-run here to show what would be reclaimed?
		// But valid prune dry-run requires the snapshot to be actually gone (or simulated gone).
		// Since we didn't delete the snapshot, prune --dry-run would show 0 reclaimed.
		// So we just inform the user.
		return nil
	}

	fmt.Println("Removal complete. Running prune to cleanup unreferenced data blobs...")

	// Auto-prune (no dry-run)
	stats, err := b.Prune(false)
	if err != nil {
		return fmt.Errorf("prune failed: %w", err)
	}
	fmt.Printf("Pruned %d unreferenced blobs, reclaimed %d bytes\n", stats.BlobsRemoved, stats.BytesRemoved)

	return nil
}

func runPruneCache(b *backup.Backup, dryRun bool) error {
	if dryRun {
		fmt.Println("[dry-run] Checking hash cache...")
	} else {
		fmt.Println("Pruning hash cache...")
	}
	count := b.HashCache.Prune()
	if dryRun {
		fmt.Printf("[dry-run] Would remove %d entries from hash cache.\n", count)
	} else {
		fmt.Printf("Removed %d entries from hash cache.\n", count)
		if count > 0 {
			// Save immediately
			if err := b.HashCache.MaybeSaveCache(); err != nil {
				return fmt.Errorf("failed to save cache: %w", err)
			}
		}
	}
	return nil
}

func runInitStore(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(absPath, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", absPath, err)
	}

	backupDir := filepath.Join(absPath, ".backup")
	if _, err := os.Stat(backupDir); err == nil {
		return fmt.Errorf("already initialized as a store (or source) at %s", absPath)
	}

	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return err
	}

	storeToml := filepath.Join(backupDir, "store.toml")
	if err := os.WriteFile(storeToml, []byte("store = \".\"\n"), 0644); err != nil {
		return fmt.Errorf("failed to write store.toml: %w", err)
	}

	// Create data and backups
	os.MkdirAll(filepath.Join(absPath, "data"), 0755)
	os.MkdirAll(filepath.Join(absPath, "snapshots"), 0755)

	fmt.Printf("Initialized backup store at %s\n", absPath)
	if err := ensureStoreReadme(absPath); err != nil {
		fmt.Printf("Warning: Failed to create README: %v\n", err)
	}
	return nil
}

func runInit(path, store, project string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(absPath, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", absPath, err)
	}

	backupDir := filepath.Join(absPath, ".backup")
	if _, err := os.Stat(backupDir); err == nil {
		// Check if config.toml exists
		if _, err := os.Stat(filepath.Join(backupDir, "config.toml")); err == nil {
			return fmt.Errorf("already initialized as a source at %s", absPath)
		}
	}

	// Interactive input if missing
	if store == "" {
		fmt.Print("Enter backup store path: ")
		fmt.Scanln(&store)
		if store == "" {
			return fmt.Errorf("store path required")
		}
	}
	if project == "" {
		project = filepath.Base(absPath)
		fmt.Printf("Enter project name [%s]: ", project)
		var input string
		fmt.Scanln(&input)
		if input != "" {
			project = input
		}
	}

	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return err
	}

	configToml := filepath.Join(backupDir, "config.toml")
	content := fmt.Sprintf("store = \"%s\"\nname = \"%s\"\n", filepath.ToSlash(store), project)
	if err := os.WriteFile(configToml, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write config.toml: %w", err)
	}

	fmt.Printf("Initialized backup source at %s (project: %s)\n", absPath, project)
	if err := ensureSourceReadme(backupDir); err != nil {
		fmt.Printf("Warning: Failed to create README: %v\n", err)
	}
	return nil
}

func ensureSourceReadme(backupDir string) error {
	readmePath := filepath.Join(backupDir, "README.md")
	if _, err := os.Stat(readmePath); err == nil {
		return nil // Already exists
	}

	content := `# Backup Source

This directory is configured as a backup source.

## Configuration
Configuration is stored in ` + "`config.toml`" + `.

## Usage
- **Backup**: Run ` + "`backup`" + ` in this directory.
- **Restore**: Run ` + "`backup restore <snapshot_id>`" + `.
- **List Snapshots**: Run ` + "`backup snapshots`" + `.

For more information, visit: https://github.com/djabi/backup
`
	return os.WriteFile(readmePath, []byte(content), 0644)
}

func ensureStoreReadme(storeRoot string) error {
	readmePath := filepath.Join(storeRoot, "README.md")
	if _, err := os.Stat(readmePath); err == nil {
		return nil // Already exists
	}

	content := `# Backup Store

This directory is a backup store containing deduplicated data and snapshots.

## Structure
- ` + "`data/`" + `: Contains content-addressed data blobs.
- ` + "`snapshots/`" + `: Contains snapshot references organized by project.

## Usage
- **Initialize Source**: ` + "`backup init --store <path/to/this/store>`" + `
- **List All Backups**: ` + "`backup snapshots --store <path/to/this/store>`" + `

For more information, visit: https://github.com/djabi/backup
`
	return os.WriteFile(readmePath, []byte(content), 0644)
}
