package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGeneratorProvenance: a world folder records how its chunks were made (profile, and whether
// Bedrock data was read with a hash of it). A start with the same kept quiet; a start with other data
// says so, naming both, and the record follows the new start.
func TestGeneratorProvenance(t *testing.T) {
	dir := t.TempDir()
	a, b := "profile=bedrock data=sha256:aaaa", "profile=bedrock data=none"
	if old, changed, err := recordProvenance(dir, a); err != nil || changed || old != "" {
		t.Fatalf("new world: old %q changed %v err %v", old, changed, err)
	}
	if old, changed, err := recordProvenance(dir, a); err != nil || changed || old != a {
		t.Fatalf("same data: old %q changed %v err %v", old, changed, err)
	}
	old, changed, err := recordProvenance(dir, b)
	if err != nil || !changed || old != a {
		t.Fatalf("other data: old %q changed %v err %v", old, changed, err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, provenanceFile))
	if strings.TrimSpace(string(got)) != b {
		t.Errorf("record %q, want %q", got, b)
	}
}
