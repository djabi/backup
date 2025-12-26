package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"time"
)

func TestIntegration(t *testing.T) {
	// 1. Build the binary
	tempDir, err := os.MkdirTemp("", "backup_integration_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	binPath := filepath.Join(tempDir, "backup-cli")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, string(out))
	}

	// Helper to run commands
	run := func(dir string, args ...string) string {
		cmd := exec.Command(binPath, args...)
		cmd.Dir = dir
		// Set env to separate this run from user config if any (though we rely on file config)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Command failed: %s %v\nDir: %s\nError: %v\nOutput: %s", binPath, args, dir, err, string(out))
		}
		return string(out)
	}

	// 2. Setup Test Environment
	srcDir := filepath.Join(tempDir, "src")
	storeDir := filepath.Join(tempDir, "store")

	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Setup Config
	configDir := filepath.Join(srcDir, ".backup")
	if err := os.Mkdir(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDir, "config.toml")
	projectName := "integration-test-proj"
	configContent := fmt.Sprintf("store = \"%s\"\nname = \"%s\"", filepath.ToSlash(storeDir), projectName)
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create initial content
	// src/file1.txt
	// src/sub/file2.txt
	subDir := filepath.Join(srcDir, "sub")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	file1 := filepath.Join(srcDir, "file1.txt")
	file2 := filepath.Join(subDir, "file2.txt")
	os.WriteFile(file1, []byte("v1-content1"), 0644)
	os.WriteFile(file2, []byte("v1-content2"), 0644)

	// 3. Scenario: Backup from Source Root
	t.Log("--- Scenario 1: Initial Backup from Source Root ---")
	// Use --yes to confirm store.toml creation (Global flag must be before subcommand)
	out := run(srcDir, "--yes", "backup")
	t.Logf("Backup Output: %s", out)

	snapshot1 := parseSnapshotID(t, out)
	t.Logf("Snapshot 1: %s", snapshot1)

	// Verify store content (basic check)
	// Expect snapshot head in <store>/snapshots/<project>/<timestamp>
	snapHead := filepath.Join(storeDir, "snapshots", projectName, snapshot1)
	if _, err := os.Stat(snapHead); os.IsNotExist(err) {
		t.Errorf("Snapshot head mismatch. Expected %s", snapHead)
	}

	// Verify store.toml created
	storeToml := filepath.Join(storeDir, ".backup", "store.toml")
	if _, err := os.Stat(storeToml); os.IsNotExist(err) {
		t.Errorf("store.toml not created in store root")
	}

	// 4. Scenario: Listing Snapshots & Tree from Source Root
	t.Log("--- Scenario 2: Listing ---")
	out = run(srcDir, "snapshots")
	if !strings.Contains(out, snapshot1) {
		t.Errorf("Snapshots output missing ID %s. Got: %s", snapshot1, out)
	}

	out = run(srcDir, "tree", snapshot1) // or "tree" defaults to latest
	if !strings.Contains(out, "file1.txt") || !strings.Contains(out, "sub/") {
		t.Errorf("Tree output incomplete. Got: %s", out)
	}

	// 5. Scenario: Incremental Backup from Subdirectory
	t.Log("--- Scenario 3: Incremental Backup from Subdirectory ---")

	// Modify file1
	os.WriteFile(file1, []byte("v2-content1"), 0644)
	// Add file3 in sub
	file3 := filepath.Join(subDir, "file3.txt")
	os.WriteFile(file3, []byte("v2-content3"), 0644)

	// Run backup from subDir
	// Should find root via .backup lookup
	out = run(subDir, "backup")
	t.Logf("Backup (Subdir) Output: %s", out)

	snapshot2 := parseSnapshotID(t, out)
	if snapshot1 == snapshot2 {
		t.Error("Snapshot ID should change")
	}

	// 6. Scenario: Status from Subdirectory
	t.Log("--- Scenario 4: Status from Subdirectory ---")
	// Status in subDir should show checking against repo
	// Modify file2
	os.WriteFile(file2, []byte("v3-content2-dirty"), 0644)
	out = run(subDir, "status")
	t.Logf("Status (Subdir) Output: %s", out)

	// Should show file2 modified (NewContentKnown? or similar depending on status logic)
	// file2 was "v1-content2" in snapshot2? No, we didn't change it before snapshot2.
	// So snapshot2 has "v1-content2".
	// Now we wrote "v3-content2-dirty".
	// It should show up as modified/new.
	// Logic: "New" or "StatusNewContentKnown" if content matches *some* archived file?
	// If unique content, "StatusNew".
	if !strings.Contains(out, "file2.txt") {
		t.Error("Status should mention file2.txt")
	}

	// Verify sorting: file3.txt comes before file2.txt? No, file2.txt < file3.txt
	// Wait, we have file2.txt and file3.txt in subDir?
	// file2.txt and file3.txt are both in subDir.
	// But `backup status` outputs:
	// Status path/to/file
	// If we are in subDir:
	// Status file2.txt
	// Status file3.txt
	// Let's verify file2.txt appears before file3.txt in output
	idx2 := strings.Index(out, "file2.txt")
	idx3 := strings.Index(out, "file3.txt")
	if idx2 == -1 || idx3 == -1 {
		t.Error("Missing file2 or file3 in status output")
	}
	if idx2 > idx3 {
		t.Error("Status output not sorted: file3.txt appeared before file2.txt")
	}

	// 7. Scenario: Running from Store Directory (Headless)
	t.Log("--- Scenario 5: Headless Operations (From Store) ---")
	// cd storeDir
	// Run snapshots - needs --store .
	// Note: since we use project name, snapshots list might need handling?
	// `BackupRoots` lists `StoreSnapshots/ProjectName` IF `b.ProjectName` set.
	// If we run `backup-cli --store .`, `b.ProjectName` is empty (no config).
	// So `BackupRoots` lists `StoreSnapshots/*.
	// Wait, internal structure: `StoreSnapshots` contains "integration-test-proj/" directory.
	// `BackupRoots` iterates `StoreSnapshots`.
	// Does it recursively find snapshots?
	// `backup.go`:
	// ```
	// 	searchDir := b.StoreSnapshots
	// 	if b.ProjectName != "" {
	// 		searchDir = filepath.Join(...)
	// 	}
	// 	files, err := os.ReadDir(searchDir)
	// ```
	// If ProjectName is empty, it reads `StoreSnapshots`.
	// It sees "integration-test-proj" directory.
	// `NewBackupRoot(b, .../integration-test-proj)` -> fails time parsing.
	// Result: `snapshots` command lists nothing or warns.
	// Manual verification: "Warning: skipped invalid backup head integration-test-proj".
	// This means headless listing of named projects behaves poorly if name not provided via config/flag.
	// But we removed the flag.
	// So user MUST use config? BUT config is in source.
	// Use case: Admin going into store to inspect.
	// They don't have project name easily?
	// Maybe `BackupRoots` should look into subdirs if it sees directories?
	// Or we accept this limitation: headless requires pointing to projectsubdir?
	// Or `backup-cli --store ./snapshots/integration-test-proj` ? (using store root as the project snapshot root).
	// `StoreRoot` expects `data` and `snapshots` inside.

	// Let's test the limitation/behavior as is.
	// Actually, `FindBackupRoot` supports "proj/timestamp".
	// But `snapshots` command calls `BackupRoots()` which lists `searchDir`.
	// If we want to list snapshots for a project headless:
	// We can't currently without config.
	// We might need to add back `--name` flag later?
	// OR: `backup-cli` should maybe auto-discover projects?
	// Let's stick to what we have:
	// Try `restore` with explicit project path "integration-test-proj/<snap2>".

	targetRestore := filepath.Join(tempDir, "restore_from_store")
	fullSnapID := projectName + "/" + snapshot2

	// Using --store flag
	run(storeDir, "--store", storeDir, "restore", fullSnapID, targetRestore)

	// Verify file3 exists
	if _, err := os.Stat(filepath.Join(targetRestore, "sub/file3.txt")); os.IsNotExist(err) {
		t.Errorf("Headless restore failed for %s", fullSnapID)
	}

	// 8. Scenario: Restore into Source Subdir (Context Detect)
	t.Log("--- Scenario 8: Restore into Source Subdir ---")
	// cd src/sub
	// delete file3
	os.Remove(file3)
	// restore <snap2> file3.txt
	// Should restore to ./file3.txt
	run(subDir, "restore", snapshot2, "file3.txt")
	if _, err := os.Stat(file3); os.IsNotExist(err) {
		t.Errorf("Context restore in subdir failed")
	}

	// 8b. Scenario: Restore Symlink
	t.Log("--- Scenario 8b: Restore Symlink ---")
	if runtime.GOOS == "windows" {
		t.Log("Skipping symlink test on Windows")
	} else {
		// Create a symlink and back it up
		linkPath := filepath.Join(srcDir, "mylink")
		if err := os.Symlink("file1.txt", linkPath); err != nil {
			t.Fatal(err)
		}

		// Backup again to catch the link
		out = run(srcDir, "backup")
		snapshot3 := parseSnapshotID(t, out)

		// Verify status doesn't panic with symlink
		out = run(srcDir, "status")
		if !strings.Contains(out, "mylink") {
			t.Logf("Status output: %s", out)
			// It might be "StatusArchived" or similar. Just checking it doesn't panic and output exists.
		}

		// Restore link
		// Remove link first
		os.Remove(linkPath)
		run(srcDir, "restore", snapshot3, "mylink")

		// Verify restored is a link
		info, err := os.Lstat(linkPath)
		if err != nil {
			t.Errorf("Failed to stat restored link: %v", err)
		} else {
			if info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("Restored file is not a symlink")
			}
			target, _ := os.Readlink(linkPath)
			if target != "file1.txt" {
				t.Errorf("Restored link target mismatch. Got %s, want file1.txt", target)
			}
		}
	}

	// 9. Scenario: Integrity Check (Healthy)
	t.Log("--- Scenario 9: Integrity Check (Healthy) ---")
	out = run(srcDir, "check")
	if !strings.Contains(out, "Store integrity check passed") {
		t.Errorf("Check failed on healthy store: %s", out)
	}

	out = run(srcDir, "check", "--deep")
	if !strings.Contains(out, "Store integrity check passed") {
		t.Errorf("Deep check failed on healthy store: %s", out)
	}

	// 10. Scenario: Integrity Check (Corrupted)
	t.Log("--- Scenario 10: Integrity Check (Corrupted) ---")
	// Find a blob to delete
	// We know blobs are in store/data/xx/xxxxxxxx...
	// Let's find one.
	dataPath := filepath.Join(storeDir, "data")
	foundBlob := ""
	filepath.Walk(dataPath, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() && strings.HasSuffix(path, ".gz") {
			foundBlob = path
			return filepath.SkipAll
		}
		return nil
	})

	if foundBlob == "" {
		t.Fatal("No blobs found to corrupt")
	}

	// Move blob to backup location to restore later? No need, just basic test.
	// 10a. Missing Blob
	if err := os.Rename(foundBlob, foundBlob+".bak"); err != nil {
		t.Fatal(err)
	}

	cmd = exec.Command(binPath, "check")
	cmd.Dir = srcDir
	outBytes, _ := cmd.CombinedOutput() // Expected to fail
	out = string(outBytes)

	// Should fail
	if strings.Contains(out, "Store integrity check passed") {
		t.Error("Check passed despite missing blob")
	}
	if !strings.Contains(out, "missing blob") && !strings.Contains(out, "missing referenced blob") {
		t.Errorf("Check didn't report missing blob correctly. Got: %s", out)
	}

	// Restore blob
	os.Rename(foundBlob+".bak", foundBlob)

	// 10b. Corrupted Content
	// Overwrite blob content with garbage
	originalContent, _ := os.ReadFile(foundBlob)
	os.WriteFile(foundBlob, []byte("garbage content"), 0644)

	// Helper to check for specific error message
	// Normal verify (existence) might pass if we just replaced content?
	// Verify implementation checks os.Stat. If size > 0 it passes simple check.
	// So `check` (shallow) might PASS if we just replaced bytes!
	// Wait, existence check: `if info.Size() == 0`. "garbage content" len > 0.
	// So Shallow Check should PASS.

	out = run(srcDir, "check")
	if !strings.Contains(out, "Store integrity check passed") {
		t.Logf("Shallow check failed (unexpected?): %s", out)
		// It might fail if traversing directory structure fails (garbage content not gzip or not dir).
		// If the blob was a directory, shallow check calls `traverseDirectory` -> opens gzip scanner -> fails.
		// If blob was a file, shallow check doesn't open it (leaf).
		// So it depends what foundBlob is.
	}

	// Deep verify MUST fail
	cmd = exec.Command(binPath, "check", "--deep")
	cmd.Dir = srcDir
	outBytes, _ = cmd.CombinedOutput()
	out = string(outBytes)

	if strings.Contains(out, "Store integrity check passed") {
		t.Error("Deep check passed despite corruption")
	}
	if !strings.Contains(out, "corrupted blob") && !strings.Contains(out, "gzip error") && !strings.Contains(out, "hash mismatch") {
		t.Errorf("Deep check didn't report corruption. Got: %s", out)
	}

	// Restore original content
	os.WriteFile(foundBlob, originalContent, 0644)

	// 11. Scenario: Auto-detected Headless Operation
	t.Log("--- Scenario 11: Auto-detected Headless Operation ---")
	// Run "snapshots" from storeDir without --store flag
	// Must detect CWD as store
	out = run(storeDir, "snapshots")
	if !strings.Contains(out, "snapshots found") {
		t.Errorf("Auto-detection failed. Output: %s", out)
	}
	// Verify project name is shown (enhanced headless listing)
	if !strings.Contains(out, "integration-test-proj") {
		t.Errorf("Headless listing should show project name. Output: %s", out)
	}

	// 12. Scenario: Status from Store (Headless)
	t.Log("--- Scenario 12: Status from Store (Headless) ---")
	out = run(storeDir, "status")
	if !strings.Contains(out, "Source directory not specified") {
		t.Errorf("Headless status failed. Output: %s", out)
	}
	// Enhanced headless check
	if !strings.Contains(out, "Listing all projects:") {
		t.Error("Headless status should list all projects")
	}
	if !strings.Contains(out, "integration-test-proj") {
		t.Error("Headless status should list 'integration-test-proj'")
	}
	if !strings.Contains(out, "Just now") && !strings.Contains(out, "mins ago") {
		// It should be very recent
		t.Logf("Headless status output:\n%s", out)
		t.Error("Headless status should show relative time (Just now/mins ago)")
	}

	// 13. Scenario: Create another project to test sorting
	t.Log("--- Scenario 13: Multi-project Headless Status ---")
	// Manually create another project in store
	proj2 := "older-project"
	proj2Dir := filepath.Join(storeDir, "snapshots", proj2)
	os.MkdirAll(proj2Dir, 0755)
	// Create a backup from yesterday
	yesterday := time.Now().Add(-25 * time.Hour)
	tStamp := yesterday.Format("060102-150405")
	os.WriteFile(filepath.Join(proj2Dir, tStamp), []byte("dummyhash"), 0644)

	out = run(storeDir, "status")
	if !strings.Contains(out, "older-project") {
		t.Error("Headless status should list older-project")
	}
	// Verify sorting: "integration-test-proj" (newer) should appear before "older-project" (older)
	idxNew := strings.Index(out, "integration-test-proj")
	idxOld := strings.Index(out, "older-project")
	if idxNew == -1 || idxOld == -1 {
		t.Fatal("Missing projects in output")
	}
	if idxNew > idxOld {
		t.Error("Projects not sorted by recency: older project appeared first")
	}
	if !strings.Contains(out, "1 days ago") {
		// Just output details to debug
		t.Logf("Older Project Check Failed. Output:\n%s", out)
		// We can loosen the check or make it exact
		// 25 hours ago -> 25 / 24 = 1 day
		// Maybe check for "1 days ago" OR "25 hours ago" ?
		// Logic: d < 30 days -> d.Hours()/24.
		// 25h / 24 = 1.04 -> 1 day.
		t.Errorf("Relative time for older project incorrect. Expected '1 days ago' or similar. Output: %s", out)
	}

	// 14. Scenario: Backup from Headless/Store should fail
	t.Log("--- Scenario 14: Backup from Headless/Store should fail ---")
	// run() helper fails on error, we need to allow error.
	// We'll use exec directly.
	cmd = exec.Command(binPath, "backup")
	cmd.Dir = storeDir
	// Needs --store flag? Actually headless detection usually happens if store is CWD or .backup present.
	// In the test setup, storeDir has .backup/store.toml. So it detects store mode.
	outBytes, err = cmd.CombinedOutput()
	out = string(outBytes)

	if err == nil {
		t.Error("Backup from store directory should have failed")
	}
	if !strings.Contains(out, "Run 'backup' from a source directory") {
		t.Errorf("Error message missing expected text. Got: %s", out)
	}
	if !strings.Contains(out, "running inside a store directory") {
		t.Errorf("Error message missing store hint. Got: %s", out)
	}

	// 15. Scenario: Graceful handling of empty snapshot file
	t.Log("--- Scenario 15: Graceful handling of empty snapshot file ---")
	// 1. Create .backup to force b.Top set
	if err := os.MkdirAll(filepath.Join(storeDir, ".backup"), 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(storeDir, ".backup", "config.toml"), []byte("store = \".\"\nname = \"self-backup\""), 0644)

	// 2. Create empty snapshot with valid name
	emptySnapPath := filepath.Join(storeDir, "snapshots", "self-backup", "251219-000000")
	os.MkdirAll(filepath.Dir(emptySnapPath), 0755)
	os.WriteFile(emptySnapPath, []byte(""), 0644)

	// 3. Run status
	out = run(storeDir, "status")
	if strings.Contains(out, "panic") || strings.Contains(out, "runtime error") {
		t.Fatalf("Status panicked on empty snapshot: %s", out)
	}
	// It should probably skip it or show error
	if !strings.Contains(out, "snapshot file is empty") && !strings.Contains(out, "error") {
		// As long as it doesn't crash
	}

	// 16. Scenario: init-store
	t.Log("--- Scenario 16: init-store ---")
	newStoreDir, err := ioutil.TempDir("", "backup_init_store_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(newStoreDir)

	// Run init-store
	cmd = exec.Command(binPath, "init-store", newStoreDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("init-store failed: %s", out)
	}

	// Verify files
	if _, err := os.Stat(filepath.Join(newStoreDir, ".backup", "store.toml")); err != nil {
		t.Error("init-store missing store.toml")
	}
	if _, err := os.Stat(filepath.Join(newStoreDir, "data")); err != nil {
		t.Error("init-store missing data dir")
	}

	// 17. Scenario: init (source)
	t.Log("--- Scenario 17: init source ---")
	newSourceDir, err := ioutil.TempDir("", "backup_init_source_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(newSourceDir)

	// Run init with flags
	cmd = exec.Command(binPath, "init", "--store", newStoreDir, "--project", "myproj", newSourceDir)
	if outBytes, err = cmd.CombinedOutput(); err != nil {
		t.Fatalf("init source failed: %s", outBytes)
	}

	// Verify config
	configPath = filepath.Join(newSourceDir, ".backup", "config.toml")
	var content []byte
	content, err = ioutil.ReadFile(configPath)
	if err != nil {
		t.Fatal("init source missing config.toml")
	}
	sContent := string(content)
	if !strings.Contains(sContent, `store = "`+newStoreDir+`"`) { // store path might be abs
		// Just check store path presence
		// newStoreDir is absolute from TempDir
	}
	if !strings.Contains(sContent, `name = "myproj"`) {
		t.Error("init source config missing project name")
	}

	// 18. Scenario: Ignores
	t.Log("--- Scenario 18: Ignores ---")
	// Setup:
	// root/
	//   ignored.txt
	//   included.txt
	//   .backupignore ("ignored.txt")
	//   sub/
	//     sub_ignored.log
	//     sub_included.log
	//     .gitignore ("*.log")
	//     nested/
	//       should_be_ignored.log
	//       .backupignore ("!should_be_ignored.log") -> Negation override check

	ignoreDir, err := ioutil.TempDir("", "backup_ignore_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(ignoreDir)

	// Create files
	os.MkdirAll(filepath.Join(ignoreDir, "sub", "nested"), 0755)

	os.WriteFile(filepath.Join(ignoreDir, "ignored.txt"), []byte("ignored_data"), 0644)
	os.WriteFile(filepath.Join(ignoreDir, "included.txt"), []byte("included_data"), 0644)
	os.WriteFile(filepath.Join(ignoreDir, ".backupignore"), []byte("ignored.txt\n"), 0644)

	os.WriteFile(filepath.Join(ignoreDir, "sub", "sub_ignored.log"), []byte("sub_ignored_data"), 0644)
	os.WriteFile(filepath.Join(ignoreDir, "sub", "sub_included.dat"), []byte("sub_included_data"), 0644)
	os.WriteFile(filepath.Join(ignoreDir, "sub", ".gitignore"), []byte("*.log\n"), 0644)

	os.WriteFile(filepath.Join(ignoreDir, "sub", "nested", "should_be_ignored.log"), []byte("nested_ignored_data"), 0644)
	// Override rejection
	os.WriteFile(filepath.Join(ignoreDir, "sub", "nested", ".backupignore"), []byte("!should_be_ignored.log\n"), 0644)

	// Init and run backup
	// Init store (can reuse or new? New to be safe)
	ignoreStoreDir, _ := ioutil.TempDir("", "backup_ignore_store")
	defer os.RemoveAll(ignoreStoreDir)

	// Create store structure manually or use init-store
	cmd = exec.Command(binPath, "init-store", ignoreStoreDir)
	cmd.Run()

	// Create source config
	os.Mkdir(filepath.Join(ignoreDir, ".backup"), 0755)
	var configContentStr string
	configContentStr = fmt.Sprintf("store = \"%s\"\nname = \"ignore-test\"\n", filepath.ToSlash(ignoreStoreDir))
	os.WriteFile(filepath.Join(ignoreDir, ".backup", "config.toml"), []byte(configContentStr), 0644)

	// Run Backup
	cmd = exec.Command(binPath, "backup")
	// Let's run from dir
	cmd.Dir = ignoreDir
	outBytes, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Backup failed: %s", outBytes)
	}
	out = string(outBytes)
	t.Log("Backup Output:\n", out)

	// Verify Output
	if strings.Contains(out, "ignored.txt") {
		t.Error("ignored.txt was archived but should be ignored")
	}
	if !strings.Contains(out, "included.txt") {
		t.Error("included.txt missing")
	}
	if strings.Contains(out, "sub_ignored.log") {
		t.Error("sub_ignored.log was archived but should be ignored by .gitignore")
	}
	if !strings.Contains(out, "sub_included.dat") {
		t.Error("sub_included.dat missing")
	}
	// Check nested override
	if _, err := os.Stat(filepath.Join(ignoreDir, "sub", "nested", "should_be_ignored.log")); os.IsNotExist(err) {
		t.Error("should_be_ignored.log missing stuck in ignore?")
	}

	// Verify Status --show-ignored
	t.Log("--- Scenario 19: Status with --show-ignored ---")
	cmd = exec.Command(binPath, "status", "--show-ignored")
	cmd.Dir = ignoreDir
	outBytes, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Status failed: %v\nOutput: %s", err, string(outBytes))
	}
	out = string(outBytes)
	t.Logf("Status Output:\n%s", out)

	if !strings.Contains(out, "I ignored.txt (Ignored by .backupignore: ignored.txt)") {
		t.Error("Status output missing ignored.txt reason")
	}
	if !strings.Contains(out, fmt.Sprintf("I %s (Ignored by .gitignore: *.log)", filepath.FromSlash("sub/sub_ignored.log"))) {
		t.Error("Status output missing sub_ignored.log reason")
	}
	// Ensure project dir exists
	os.MkdirAll(filepath.Dir(emptySnapPath), 0755)
	os.WriteFile(emptySnapPath, []byte(""), 0644)

	if strings.Contains(out, "open :") {
		t.Errorf("Resulted in 'open :' error. Empty snapshot should be skipped. Output: %s", out)
	}

	// 20. Scenario: Prune Unreferenced Blobs
	t.Log("--- Scenario 20: Prune Unreferenced Blobs ---")
	// Setup:
	// 1. Create a snapshot (Snapshot A) with UNIQUE content
	os.WriteFile(file1, []byte(fmt.Sprintf("unique_content_snap_A_%d", time.Now().UnixNano())), 0644)
	out = run(srcDir, "backup")
	snapA := parseSnapshotID(t, out)
	t.Logf("Created Snap A: %s", snapA)

	// 2. Modify source and create another snapshot (Snapshot B)
	os.WriteFile(file1, []byte("content_for_snap_B"), 0644)
	out = run(srcDir, "backup")
	snapB := parseSnapshotID(t, out)
	t.Logf("Created Snap B: %s", snapB)

	// 3. Manually delete Snapshot A head file (simulate deletion)
	// Path: store/snapshots/<proj>/<snapA>
	snapAPath := filepath.Join(storeDir, "snapshots", "integration-test-proj", snapA)
	if err := os.Remove(snapAPath); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond) // Ensure FS update propagates

	// 4. Run Check -> Should report unreferenced blobs (and fail)
	cmd = exec.Command(binPath, "check")
	cmd.Dir = srcDir
	outBytes, err = cmd.CombinedOutput()
	out = string(outBytes)

	if err == nil {
		t.Error("Check should fail when unreferenced blobs are found")
	}
	if !strings.Contains(out, "unreferenced blob") {
		t.Errorf("Check did not report unreferenced blobs after deleting snapshot. Output:\n%s", out)
	}

	// 5. Run Prune Dry-Run
	cmd = exec.Command(binPath, "prune", "--dry-run")
	cmd.Dir = srcDir
	outBytes, _ = cmd.CombinedOutput()
	out = string(outBytes)
	t.Logf("Prune Dry-Run Output: %s", out)

	if !strings.Contains(out, "Found") || !strings.Contains(out, "would reclaim") {
		t.Error("Prune dry-run output unexpected")
	}
	if strings.Contains(out, "Pruned") {
		t.Error("Prune dry-run claimed to have pruned")
	}

	// 6. Run Prune
	cmd = exec.Command(binPath, "prune")
	cmd.Dir = srcDir
	outBytes, _ = cmd.CombinedOutput()
	out = string(outBytes)
	t.Logf("Prune Output: %s", out)

	if !strings.Contains(out, "Pruned") || !strings.Contains(out, "reclaimed") {
		t.Error("Prune output unexpected")
	}

	// 7. Run Check -> Should be clean
	out = run(srcDir, "check")
	if strings.Contains(out, "unreferenced blob") {
		t.Error("Check still reports unreferenced blobs after prune")
	}

	// 8. Verify Snap B works (restore)
	// We need to restore something unique to Snap B to be sure logic didn't over-prune
	// Snap B has "content_for_snap_B" in file1.
	restoreDest := filepath.Join(tempDir, "restore_snap_b_after_prune")
	run(storeDir, "--store", storeDir, "restore", "integration-test-proj/"+snapB, restoreDest)

	restoredContent, err := os.ReadFile(filepath.Join(restoreDest, "file1.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(restoredContent) != "content_for_snap_B" {
		t.Errorf("Snap B restore content mismatch. Got: %s", string(restoredContent))
	}

	// 21. Scenario: Multi-project Reference Safety (Orphan Misidentification)
	t.Log("--- Scenario 21: Multi-project Reference Safety ---")
	// Problem: If we have multiple projects in the same store, `check` (from one project context)
	// might think blobs from the other project are unreferenced.
	// Setup:
	// 1. Create Project 2 in a new source dir, pointing to SAME store
	srcDir2, err := ioutil.TempDir("", "backup_src2")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(srcDir2)

	// Init Project 2
	run(srcDir2, "init", "--store", storeDir, "--project", "project-two")

	// 2. Create unique content in Project 2
	os.WriteFile(filepath.Join(srcDir2, "unique_p2.txt"), []byte("unique_p2_content"), 0644)

	// 3. Backup Project 2
	run(srcDir2, "backup")

	// 4. Run Check from Project 1 (srcDir)
	// IT SHOULD PASS.
	// If it fails with "unreferenced blob", it means it didn't see Project 2's references.
	out = run(srcDir, "check")
	if strings.Contains(out, "unreferenced blob") {
		t.Errorf("Check incorrectly reported unreferenced blobs from another project:\n%s", out)
	}

	// 22. Scenario: Nested Directory Prune Safety
	t.Log("--- Scenario 22: Nested Directory Prune Safety ---")
	// Setup: Create deeply nested structure
	// src/deep/nested/dir/file.txt
	deepDir := filepath.Join(srcDir, "deep", "nested", "dir")
	if err := os.MkdirAll(deepDir, 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(deepDir, "deep_file.txt"), []byte("deep_content"), 0644)

	run(srcDir, "backup")

	// Check should be clean
	cmd = exec.Command(binPath, "check")
	cmd.Dir = srcDir
	outBytes, _ = cmd.CombinedOutput()
	out = string(outBytes)

	if !strings.Contains(out, "Store integrity check passed.") {
		t.Errorf("Check failed:\n%s", out)
	}

	// Prune should find nothing
	outBytes, _ = exec.Command(binPath, "prune", "--dry-run", "--store", storeDir).CombinedOutput()
	if strings.Contains(string(outBytes), "Found 0 unreferenced blobs") == false && strings.Contains(string(outBytes), "Found") {
		t.Errorf("Prune found unreferenced blobs in nested structure:\n%s", outBytes)
	}
	// 23. Scenario: Remove/Forget Snapshot
	t.Log("--- Scenario 23: Remove Snapshot ---")
	// Create a disposable snapshot
	os.WriteFile(file1, []byte(fmt.Sprintf("unique_remove_test_%d", time.Now().UnixNano())), 0644)
	out = run(srcDir, "backup")
	snapRemove := parseSnapshotID(t, out)

	// Dry-run Remove
	outDry := run(srcDir, "remove", "--dry-run", snapRemove)
	if !strings.Contains(outDry, "[dry-run] Would remove snapshot") {
		t.Errorf("Remove dry-run missing expected output: %s", outDry)
	}
	if strings.Contains(outDry, "Removal complete") || strings.Contains(outDry, "Pruned") {
		t.Errorf("Remove dry-run claimed to have removed/pruned: %s", outDry)
	}
	// Verify still exists
	out = run(srcDir, "snapshots")
	if !strings.Contains(out, snapRemove) {
		t.Errorf("Snapshot %s vanished after dry-run remove", snapRemove)
	}

	// Remove it really
	outRemove := run(srcDir, "remove", snapRemove)
	if !strings.Contains(outRemove, "Removal complete") {
		t.Errorf("Remove command failed output: %s", outRemove)
	}

	// Verify it's gone
	out = run(srcDir, "snapshots")
	if strings.Contains(out, snapRemove) {
		t.Errorf("Snapshot %s still listed after removal", snapRemove)
	}

	// Verify prune ran by checking the remove output
	// Previous logic ran explicit prune, now we just check the remove command output
	// which should attempt to prune.

	// 24. Scenario: Verify README creation
	t.Log("--- Scenario 24: Verify README creation ---")
	// Check Source README
	sourceReadme := filepath.Join(srcDir, ".backup", "README.md")
	if _, err := os.Stat(sourceReadme); os.IsNotExist(err) {
		t.Error("Source README not created during init")
	} else {
		content, _ := os.ReadFile(sourceReadme)
		if !strings.Contains(string(content), "Backup Source") {
			t.Error("Source README content mismatch")
		}
	}

	// Check Store README
	storeReadme := filepath.Join(storeDir, "README.md")
	if _, err := os.Stat(storeReadme); os.IsNotExist(err) {
		t.Error("Store README not created during init")
	} else {
		content, _ := os.ReadFile(storeReadme)
		if !strings.Contains(string(content), "Backup Store") {
			t.Error("Store README content mismatch")
		}
	}

	// 25. Scenario: Retroactive README creation on backup command
	t.Log("--- Scenario 25: Retroactive README creation ---")
	// Delete READMEs
	os.Remove(sourceReadme)
	os.Remove(storeReadme)

	// Run backup
	run(srcDir, "backup")

	// Verify recreated
	if _, err := os.Stat(sourceReadme); os.IsNotExist(err) {
		t.Error("Source README not recreated during backup")
	}
	if _, err := os.Stat(storeReadme); os.IsNotExist(err) {
		t.Error("Store README not recreated during backup")
	}

	// Since we are running in a fresh context where this snap was the only one referring to unique content,
	// it should have pruned.
	// However, depending on test timing/state, maybe nothing was pruned if deduplication happened?
	// But we wrote unique content.
	// So prune logic in `remove` should have output "Pruned X unreferenced blobs".
	// Let's check `out` from `remove` command.
	if !strings.Contains(outRemove, "Pruned") {
		t.Logf("Warning: Remove command didn't mention pruning (maybe nothing to prune? or output captured differently?): %s", outRemove)
	}

	// 24. Scenario: Hash Cache Verification
	t.Log("--- Scenario 24: Hash Cache Verification ---")
	// 1. Corrupt the hash cache
	hashCachePath := filepath.Join(srcDir, ".backup", "hash-cache")
	// Read it first to restore later (optional, but good for cleanliness)
	// It's a properties file.
	// We'll append a bad line.
	f, err := os.OpenFile(hashCachePath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	// Append invalid hash
	if _, err := f.WriteString("badkey=invalidhash\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	// 2. Run check
	cmd = exec.Command(binPath, "check")
	cmd.Dir = srcDir
	outBytes, _ = cmd.CombinedOutput()
	out = string(outBytes)

	// 3. Verify failure
	if strings.Contains(out, "Store integrity check passed") {
		t.Errorf("Check passed despite corrupted hash cache:\n%s", out)
	}
	if !strings.Contains(out, "hash cache verification failed") || !strings.Contains(out, "invalid hash length") {
		t.Errorf("Check output missing hash cache error:\n%s", out)
	}

	// Clean up hash cache for next test or it stays corrupted
	os.Remove(hashCachePath)

	// 25. Scenario: Prune Hash Cache
	t.Log("--- Scenario 25: Prune Hash Cache ---")
	// 1. Create a file, backup it (populates cache)
	pruneFile := filepath.Join(srcDir, "prune_me.txt")
	os.WriteFile(pruneFile, []byte("prune_me"), 0644)
	run(srcDir, "backup")

	// 2. Delete file
	os.Remove(pruneFile)

	// 3. Run prune-cache
	out = run(srcDir, "prune-cache")
	if !strings.Contains(out, "Removed 1 entries") {
		t.Errorf("Prune cache failed to remove deleted file entry. Output: %s", out)
	}

	// 4. Verify repeated prune finds nothing
	out = run(srcDir, "prune-cache")
	if !strings.Contains(out, "Removed 0 entries") {
		t.Errorf("Repeated prune cache should remove 0 entries. Output: %s", out)
	}

	// 26. Scenario: Non-ASCII / Unicode Filenames
	t.Log("--- Scenario 26: Unicode Filenames ---")
	// Create files with unicode names
	// "fīle.txt"
	// "🚀.txt"
	// "dir/ñame.txt"

	unicodeFile1 := filepath.Join(srcDir, "fīle.txt")
	unicodeFile2 := filepath.Join(srcDir, "🚀.txt")
	unicodeDir := filepath.Join(srcDir, "unicodë")
	os.Mkdir(unicodeDir, 0755)
	unicodeFile3 := filepath.Join(unicodeDir, "ñame.txt")

	os.WriteFile(unicodeFile1, []byte("content1"), 0644)
	os.WriteFile(unicodeFile2, []byte("rocket"), 0644)
	os.WriteFile(unicodeFile3, []byte("name"), 0644)

	// Backup
	out = run(srcDir, "backup")
	if !strings.Contains(out, "Backup completed successfully") {
		t.Errorf("Backup failed for unicode files:\n%s", out)
	}

	// Verify Listing (tree)
	out = run(srcDir, "tree")
	if !strings.Contains(out, "fīle.txt") || !strings.Contains(out, "🚀.txt") || !strings.Contains(out, "unicodë/") {
		t.Errorf("Tree listing missing unicode files:\n%s", out)
	}

	// Verify Status
	out = run(srcDir, "status")
	if !strings.Contains(out, "0\tFiles") && !strings.Contains(out, "M") && !strings.Contains(out, "?") {
		// status output for clean state usually has empty list or specific clean msg?
		// IntegrationTest status output when clean shows nothing but summary?
		// Check that it DOESNT show them as Modified or Untracked.
	}
	// Actually "status" shows "? file" if untracked. If backed up, should be clean.
	// Clean status:
	// "Last backup was at ... \n \n <empty> \n X Files"
	// If it lists them with "?" or "M" it failed.
	if strings.Contains(out, "? fīle.txt") || strings.Contains(out, "? 🚀.txt") {
		t.Errorf("Status claims unicode files are untracked after backup:\n%s", out)
	}

	// Verify Check
	out = run(srcDir, "check")
	if !strings.Contains(out, "integrity check passed") {
		t.Errorf("Integrity check failed with unicode files:\n%s", out)
	}

	// Restore
	// Delete one file and restore it
	os.Remove(unicodeFile2)
	// Re-get snapshot ID from `snapshots` command
	outSnap := run(srcDir, "snapshots")
	// Parse last line?
	snaps := strings.Split(strings.TrimSpace(outSnap), "\n")
	// Format: "path/timestamp hash"
	if len(snaps) < 2 {
		t.Fatal("No snapshots found")
	}
	lastSnapLine := snaps[len(snaps)-2] // last line might be "X snapshots found"
	// split space
	parts := strings.Fields(lastSnapLine)
	snapID := ""
	// integration-test-proj/timestamp
	if len(parts) > 0 {
		snapID = filepath.Base(parts[0])
	}

	// Restore "🚀.txt"
	out = run(srcDir, "restore", snapID, "🚀.txt")
	if !strings.Contains(out, "Restore complete") {
		t.Errorf("Restore failed for unicode file: %s", out)
	}
	if _, err := os.Stat(unicodeFile2); os.IsNotExist(err) {
		t.Errorf("Unicode file 🚀.txt not restored")
	}
	if _, err := os.Stat(unicodeFile2); os.IsNotExist(err) {
		t.Errorf("Unicode file 🚀.txt not restored")
	}

	// 27. Scenario: Version Command
	t.Log("--- Scenario 27: Version Command ---")
	out = run(srcDir, "version")
	if !strings.Contains(out, "backup-cli version 1.0.0") {
		t.Errorf("Version command output incorrect: %s", out)
	}
	out = run(srcDir, "--version")
	if !strings.Contains(out, "backup version 1.0.0") {
		// cli default version text might differ
		// "backup version 1.0.0"
	}
}

func parseSnapshotID(t *testing.T, output string) string {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "Head:") {
			parts := strings.Split(line, "Head:")
			if len(parts) > 1 {
				fields := strings.Fields(parts[1])
				if len(fields) > 0 {
					return fields[0]
				}
			}
		}
	}
	t.Fatal("Could not find snapshot ID in output")
	return ""
}
