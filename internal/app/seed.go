package app

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// seedFile is where a world keeps its seed, inside the world folder.
const seedFile = "overmind-seed.txt"

// worldSeed returns the seed of the world in folder. A world that has one keeps it: asking for a
// different one is an error, because chunks already saved would no longer fit the new terrain.
// A new world takes the wanted seed, or a random one, and writes it down.
func worldSeed(folder string, want *int64) (int64, error) {
	path := filepath.Join(folder, seedFile)
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		stored, perr := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		if perr != nil {
			return 0, fmt.Errorf("read %s: %w", path, perr)
		}
		if want != nil && *want != stored {
			return 0, fmt.Errorf("world %q was made with seed %d, but the settings ask for seed %d: remove the seed setting or use a new world folder", folder, stored, *want)
		}
		return stored, nil
	case !errors.Is(err, os.ErrNotExist):
		return 0, err
	}
	var seed int64
	if want != nil {
		seed = *want
	} else {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return 0, fmt.Errorf("choose a seed: %w", err)
		}
		seed = int64(binary.LittleEndian.Uint64(b[:]))
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return 0, err
	}
	if err := os.WriteFile(path, []byte(strconv.FormatInt(seed, 10)+"\n"), 0o644); err != nil {
		return 0, err
	}
	return seed, nil
}

// seedFileMissing says whether folder holds a saved world (level.dat) but no seed file.
func seedFileMissing(folder string) bool {
	_, levelErr := os.Stat(filepath.Join(folder, "level.dat"))
	_, seedErr := os.Stat(filepath.Join(folder, seedFile))
	return levelErr == nil && errors.Is(seedErr, os.ErrNotExist)
}
