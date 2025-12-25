package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type BackupStatus int

const (
	StatusUnknown                BackupStatus = iota
	StatusArchived                            // .
	StatusArchivedContentMissing              // E
	StatusNew                                 // N
	StatusNewContentKnown                     // n
)

func (s BackupStatus) String() string {
	switch s {
	case StatusArchived:
		return "."
	case StatusArchivedContentMissing:
		return "E"
	case StatusNew:
		return "N"
	case StatusNewContentKnown:
		return "n"
	default:
		return "?"
	}
}

func (s BackupStatus) Description() string {
	switch s {
	case StatusArchived:
		return "File or directory archived"
	case StatusArchivedContentMissing:
		return "Archived, the archive content file is missing"
	case StatusNew:
		return "New file or directory, needs to be archived"
	case StatusNewContentKnown:
		return "New file or directory, content previously archived"
	default:
		return "Unknown status"
	}
}

type StatusReport struct {
	Files       int
	Directories int
	Ignored     int
	Counters    map[BackupStatus]int
}

func NewStatusReport() *StatusReport {
	return &StatusReport{
		Counters: make(map[BackupStatus]int),
	}
}

func (b *Backup) Status(showIgnored bool) error {
	latest, err := b.LatestBackupRoot()
	if err != nil {
		return err
	}

	if latest == nil {
		fmt.Println("No previous backups")
	} else {
		fmt.Printf("Last backup was at %s\n", latest)
	}
	fmt.Println()

	// If running headless (no source context), stop here.
	if b.Top == "" {
		fmt.Println("Source directory not specified (headless mode). Listing all projects:")
		return b.printHeadlessStatus()
	}

	// Current directory entry
	currentDir := NewDirectoryEntry(b, b.CurrentWorkingDir, nil)

	// We need relative path from Top to CurrentWorkingDir to locate it in BackupRoot.
	relPath, err := filepath.Rel(b.Top, b.CurrentWorkingDir)
	if err != nil {
		return err
	}

	var backupDir *BackupDirectory
	if latest != nil {
		backupDir, err = latest.LocateDirectory(relPath)
		if err != nil {
			return err
		}
	}

	report := NewStatusReport()
	if err := b.runStatus(latest, currentDir, backupDir, report, showIgnored); err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("\t%d\tFiles\n", report.Files)
	fmt.Printf("\t%d\tDirectories\n", report.Directories)

	for _, status := range []BackupStatus{StatusArchived, StatusArchivedContentMissing, StatusNew, StatusNewContentKnown} {
		count := report.Counters[status]
		if count > 0 {
			fmt.Printf("%s\t%d\t%s\n", status, count, status.Description())
		}
	}

	if showIgnored {
		fmt.Printf("I\t%d\tIgnored files\n", report.Ignored)
	}

	return nil
}

func (b *Backup) runStatus(latest *BackupRoot, current *DirectoryEntry, backupDir *BackupDirectory, report *StatusReport, showIgnored bool) error {
	// Get current entries (filesystem)
	currentEntries, err := current.Content()
	if err != nil {
		return err
	}
	sort.Slice(currentEntries, func(i, j int) bool {
		return currentEntries[i].Name() < currentEntries[j].Name()
	})

	// Print ignored if requested
	if showIgnored {
		ignored, err := current.Ignored()
		if err != nil {
			return err
		}
		sort.Slice(ignored, func(i, j int) bool {
			return ignored[i].Name < ignored[j].Name
		})
		for _, e := range ignored {
			reason := ""
			if e.Reason != nil {
				reason = fmt.Sprintf(" (Ignored by %s: %s)", e.Reason.Source, e.Reason.raw)
			}
			relName, _ := filepath.Rel(b.CurrentWorkingDir, e.Path)
			fmt.Printf("I %s%s\n", relName, reason)
			report.Ignored++
		}
	}

	// Get backup entires (store)
	var backupEntries map[string]BackupEntry
	if backupDir != nil {
		backupEntries, err = backupDir.Entries()
		if err != nil {
			return err
		}
	}

	for _, entry := range currentEntries {
		name := entry.Name()
		var status BackupStatus = StatusUnknown

		inLatest := false
		if backupEntries != nil {
			_, inLatest = backupEntries[name]
		}

		// Check if content is saved in store
		h, err := entry.Hash()
		if err != nil {
			return err
		}
		contentPath := b.Store.DataStore(h)
		contentExists := false
		if _, err := os.Stat(contentPath); err == nil {
			contentExists = true
		}

		dirEntry, isDir := entry.(*DirectoryEntry)

		if inLatest {
			if contentExists {
				status = StatusArchived
			} else {
				if isDir {
					// Recursive check for dir
					allSaved, err := dirEntry.AllFilesContentIsSaved()
					if err != nil {
						return err
					}
					if allSaved {
						status = StatusArchivedContentMissing
					} else {
						status = StatusNewContentKnown
					}
				} else {
					status = StatusArchivedContentMissing
				}
			}
		} else {
			// Not in latest
			if contentExists {
				status = StatusNewContentKnown
			} else {
				status = StatusNew
			}
		}

		report.Counters[status]++

		extra := ""
		if status == StatusArchivedContentMissing {
			extra = " #" + contentPath
		}

		if isDir {
			relName, _ := filepath.Rel(b.CurrentWorkingDir, dirEntry.path)
			report.Directories++
			fmt.Printf("%s %s/%s\n", status, relName, extra)

			// Recursion
			var subBackupDir *BackupDirectory
			if inLatest {
				subEntry := backupEntries[name]
				if bd, ok := subEntry.(*BackupDirectory); ok {
					subBackupDir = bd
				}
			}
			if err := b.runStatus(latest, dirEntry, subBackupDir, report, showIgnored); err != nil {
				return err
			}

		} else if linkEntry, ok := entry.(*LinkEntry); ok {
			relName, _ := filepath.Rel(b.CurrentWorkingDir, linkEntry.path)
			report.Files++ // Or report.Links++? Using Files for now as per Save()
			fmt.Printf("%s %s%s\n", status, relName, extra)
		} else {
			// For files, we need path accessible
			fileEntry := entry.(*FileEntry)
			relName, _ := filepath.Rel(b.CurrentWorkingDir, fileEntry.path)
			report.Files++
			fmt.Printf("%s %s%s\n", status, relName, extra)
		}
	}
	return nil
}

// AllFilesContentIsSaved checks if all files in directory (recursively) are saved.
func (d *DirectoryEntry) AllFilesContentIsSaved() (bool, error) {
	contents, err := d.Content()
	if err != nil {
		return false, err
	}
	for _, e := range contents {
		h, err := e.Hash()
		if err != nil {
			return false, err
		}
		dest := d.b.Store.DataStore(h)
		if _, err := os.Stat(dest); os.IsNotExist(err) {
			return false, nil
		}

		if dir, ok := e.(*DirectoryEntry); ok {
			saved, err := dir.AllFilesContentIsSaved()
			if err != nil {
				return false, err
			}
			if !saved {
				return false, nil
			}
		}
	}
	return true, nil
}

type ProjectStatus struct {
	Name       string
	LastBackup time.Time
}

func (b *Backup) printHeadlessStatus() error {
	projects, err := b.ListProjects()
	if err != nil {
		return err
	}

	var stats []ProjectStatus

	for _, p := range projects {
		// Create a temporary backup object with this project name to find roots?
		// Or just list files manually?
		// Reuse NewBackupRoot logic?
		// The `Backup` object `b` has empty ProjectName.
		// If we set b.ProjectName temporarily?
		// No, `BackupRoots` uses `b.ProjectName`.
		// Let's manually look into the project dir.

		projectDir := filepath.Join(b.StoreSnapshots, p)
		files, err := os.ReadDir(projectDir)
		if err != nil {
			continue // Skip bad projects
		}

		var latestTime time.Time
		found := false

		// Find latest valid timestamp
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			t, err := time.ParseInLocation("060102-150405", f.Name(), time.Local)
			if err != nil {
				continue
			}
			if !found || t.After(latestTime) {
				latestTime = t
				found = true
			}
		}

		if found {
			stats = append(stats, ProjectStatus{Name: p, LastBackup: latestTime})
		}
	}

	// Sort by recency (newest first)
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].LastBackup.After(stats[j].LastBackup)
	})

	fmt.Println()
	if len(stats) == 0 {
		fmt.Println("No backups found.")
		return nil
	}

	// Simple column printing
	// Calculate max name length for padding
	maxLen := 0
	for _, s := range stats {
		if len(s.Name) > maxLen {
			maxLen = len(s.Name)
		}
	}

	format := fmt.Sprintf("%%-%ds  %%s  %%s\n", maxLen)

	// Header?
	// fmt.Printf(format, "PROJECT", "LAST BACKUP", "AGO")

	for _, s := range stats {
		fmt.Printf(format, s.Name, s.LastBackup.Format("2006-01-02 15:04:05"), timeAgo(s.LastBackup))
	}

	return nil
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "Just now"
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		return fmt.Sprintf("%d mins ago", mins)
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		return fmt.Sprintf("%d hours ago", hours)
	}
	if d < 30*24*time.Hour {
		days := int(d.Hours() / 24)
		return fmt.Sprintf("%d days ago", days)
	}
	months := int(d.Hours() / 24 / 30)
	return fmt.Sprintf("%d months ago", months)
}
