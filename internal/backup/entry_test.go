package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileEntry_Save(t *testing.T) {
	// Setup environment
	sourceDir, err := os.MkdirTemp("", "entry_test_source")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sourceDir)

	storeDir, err := os.MkdirTemp("", "entry_test_store")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(storeDir)

	// Initialize Backup struct manually or via NewBackup helper if convenient
	// but manual is easier to control for unit test without full config.
	b := &Backup{
		Top:            sourceDir,
		StoreRoot:      storeDir,
		StoreData:      filepath.Join(storeDir, "data"),
		StoreSnapshots: filepath.Join(storeDir, "snapshots"),
		HashCache:      &HashCache{top: sourceDir, cache: make(map[string]string)},
	}
	// Need to init store structure
	b.Store = NewStore(b)
	os.MkdirAll(b.StoreData, 0755)

	// Create a test file
	filePath := filepath.Join(sourceDir, "test.txt")
	content := []byte("hello world")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatal(err)
	}

	fileEntry, err := NewFileEntry(b, filePath)
	if err != nil {
		t.Fatalf("NewFileEntry failed: %v", err)
	}
	if err := fileEntry.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	h, err := fileEntry.Hash()
	if err != nil {
		t.Fatal(err)
	}

	// Verify it exists in store
	expectedPath := b.Store.DataStore(h)
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("File not found in store at %s", expectedPath)
	}
}

func TestDirectoryEntry_Hash(t *testing.T) {
	sourceDir, err := os.MkdirTemp("", "entry_test_dir")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sourceDir)

	// Create a subdir and file
	subDir := filepath.Join(sourceDir, "sub")
	os.Mkdir(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, "file.txt"), []byte("foo"), 0644)

	// Minimal backup object
	b := &Backup{Top: sourceDir, HashCache: &HashCache{top: sourceDir, cache: make(map[string]string)}}

	dirEntry := NewDirectoryEntry(b, sourceDir, nil)
	h, err := dirEntry.Hash()
	if err != nil {
		t.Fatalf("Hash failed: %v", err)
	}
	if h == "" {
		t.Error("Hash shouldn't be empty")
	}
}
