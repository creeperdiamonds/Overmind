package app

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSeedFileMissingFromOldWorld: a world folder with a level.dat but no seed file was made without
// Overmind's seed (or the file was lost), so a new seed would not fit its saved chunks. Start warns
// then; a new folder, or one with both files, gives no warning.
func TestSeedFileMissingFromOldWorld(t *testing.T) {
	dir := t.TempDir()
	if seedFileMissing(dir) {
		t.Error("an empty folder warned")
	}
	if err := os.WriteFile(filepath.Join(dir, "level.dat"), []byte{0}, 0o644); err != nil {
		t.Fatal(err)
	}
	if !seedFileMissing(dir) {
		t.Error("a level.dat with no seed file gave no warning")
	}
	if err := os.WriteFile(filepath.Join(dir, seedFile), []byte("5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if seedFileMissing(dir) {
		t.Error("a world with both files warned")
	}
}
