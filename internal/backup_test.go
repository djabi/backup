package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestNewBackup_WithSourceDir(t *testing.T) {
	// Setup temporary test environment
	tempDir, err := os.MkdirTemp("", "backup_test_source")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	backupDir := filepath.Join(tempDir, ".backup")
	if err := os.Mkdir(backupDir, 0755); err != nil {
		t.Fatal(err)
	}

	configFile := filepath.Join(backupDir, "config.toml")
	storeDir := filepath.Join(tempDir, "store") // Relative to top
	configContent := "store = \"store\""
	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create the store directory as expected by validations
	if err := os.Mkdir(storeDir, 0755); err != nil {
		t.Fatal(err)
	}

	b, err := NewBackup(tempDir, "", true)
	if err != nil {
		t.Fatalf("NewBackup failed: %v", err)
	}

	if b.Top != tempDir {
		t.Errorf("Expected Top to be %s, got %s", tempDir, b.Top)
	}
	/*
	   // StoreRoot path resolution might vary (symlinks on mac /var vs /private/var), so we might check if it ends with "store"
	   // or resolve eval symlinks.
	   // For simplicity let's check base name.
	*/
	if filepath.Base(b.StoreRoot) != "store" {
		t.Errorf("Expected StoreRoot to end with 'store', got %s", b.StoreRoot)
	}
}

func TestNewBackup_WithStoreDir(t *testing.T) {
	tempStore, err := os.MkdirTemp("", "backup_test_store")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempStore)

	defer os.RemoveAll(tempStore)

	// Use a clean temp dir as startDir to avoid finding .backup in parent directories
	cleanSource, err := os.MkdirTemp("", "clean_source")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(cleanSource)

	b, err := NewBackup(cleanSource, tempStore, true)
	if err != nil {
		t.Fatalf("NewBackup failed: %v", err)
	}

	if b.StoreRoot != tempStore {
		t.Errorf("Expected StoreRoot to be %s, got %s", tempStore, b.StoreRoot)
	}
	if b.Top != "" {
		t.Errorf("Expected Top to be empty, got %s", b.Top)
	}
}

func TestNewBackup_Invalid(t *testing.T) {
	// Use a clean temp dir as startDir to avoid finding .backup in parent directories
	cleanSource, err := os.MkdirTemp("", "clean_source_invalid")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(cleanSource)

	// missing store and not in backup dir
	_, err = NewBackup(cleanSource, "", true)
	if err == nil {
		t.Error("Expected error when no store and no source dir provided")
	}
}

func TestNewBackup_CreatesStoreToml(t *testing.T) {
	tempStore, err := os.MkdirTemp("", "backup_test_store_init")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempStore)

	// Use a clean temp dir to avoid accidental config detection
	cleanSource, err := os.MkdirTemp("", "clean_source_init")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(cleanSource)

	// initializes store
	_, err = NewBackup(cleanSource, tempStore, true)
	if err != nil {
		t.Fatalf("NewBackup failed: %v", err)
	}

	configFile := filepath.Join(tempStore, ".backup", "store.toml")
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		t.Error("NewBackup should have created .backup/store.toml in the store root")
	}
}

func TestNewBackup_InStoreDir(t *testing.T) {
	tempStore, err := os.MkdirTemp("", "backup_test_store_cwd")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempStore)

	// Manually set up store structure (simulating existing store)
	if err := os.MkdirAll(filepath.Join(tempStore, ".backup"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempStore, ".backup", "store.toml"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	// Run NewBackup mocking CWD as tempStore
	b, err := NewBackup(tempStore, "", true)
	if err != nil {
		t.Fatalf("NewBackup failed: %v", err)
	}

	if b.StoreRoot != tempStore {
		t.Errorf("Expected StoreRoot to be %s, got %s", tempStore, b.StoreRoot)
	}
	if b.Top != "" {
		t.Errorf("Expected Top to be empty (headless mode), got %s", b.Top)
	}
}

func TestNewBackup_NonInteractive_Failure(t *testing.T) {
	tempStore, err := os.MkdirTemp("", "backup_test_store_ni")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempStore)

	// Mock Stdin with a pipe (which is not a CharDevice)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		w.Close()
		r.Close()
	}()

	// Use a clean temp dir
	cleanSource, err := os.MkdirTemp("", "clean_source_ni")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(cleanSource)

	// NewBackup with assumeYes=false
	_, err = NewBackup(cleanSource, tempStore, false)
	if err == nil {
		t.Error("Expected error when running non-interactively without --yes")
	} else if err.Error() != fmt.Sprintf("store configuration missing in %s and running non-interactively; use --yes to create", tempStore) {
		t.Errorf("Unexpected error message: %v", err)
	}
}
