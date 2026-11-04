package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// provenanceFile records, inside the world folder, how the world's chunks are made: the generator's
// version, the profile and whether Bedrock's feature data was read, with a hash of it. Chunks made with other data do not fit
// the ones saved before, so a start whose record differs warns that new chunks will seam against old.
const provenanceFile = "overmind-generator.txt"

// provenanceIdent is the record for a start: generator version, profile and data.
func provenanceIdent(profile string, bd *worldgen.BedrockData) string {
	data := "none"
	if bd != nil {
		data = "sha256:" + bd.Hash()
	}
	return "generator=" + worldgen.GeneratorVersion + " profile=" + profile + " data=" + data
}

// recordProvenance writes the current record and gives the one there was ("" for none) and whether it
// differs.
func recordProvenance(folder, current string) (old string, changed bool, err error) {
	path := filepath.Join(folder, provenanceFile)
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		old = strings.TrimSpace(string(data))
	case !errors.Is(err, os.ErrNotExist):
		return "", false, err
	}
	changed = old != "" && old != current
	if old == current {
		return old, false, nil
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return old, changed, err
	}
	return old, changed, os.WriteFile(path, []byte(current+"\n"), 0o644)
}
