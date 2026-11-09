package worldgen

import (
	"path/filepath"
	"testing"
)

// TestMangroveSwampMatchesBDS scores the blocks of Bedrock's mangrove trees in the study's mangrove
// swamp window (golden-bedrock/whole/windows/mangrove_swamp_20, 9 x 9 chunks) against BDS on the
// stable mask: for each block a mangrove places, BDS's count, ours, and how many agree (recall and
// precision), with the window's whole match. Nothing is held: the figures measure the mangrove
// (NOTES-bedrock-exact T17, T27, T51).
func TestMangroveSwampMatchesBDS(t *testing.T) {
	dir := goldenBedrockDir(t)
	testBedrockData(t)
	f := filepath.Join(dir, "whole", "windows", "mangrove_swamp_20.bin.gz")
	w := loadBDSFile(t, f)
	defer bdsWorlds.Delete(f)
	var pos [][2]int32
	for k := range w.chunks {
		pos = append(pos, [2]int32{int32(k[0]), int32(k[1])})
	}
	got := generateNames(NewWithProfile(w.seed, ProfileBedrock), pos)
	names := []string{"mangrove_log", "mangrove_roots", "muddy_mangrove_roots", "mangrove_leaves", "vine", "mangrove_propagule",
		"moss_carpet", "mud", "water", "air", "bee_nest"}
	type tally struct{ bds, ours, both int }
	all := map[string]*tally{}
	for _, n := range names {
		all[n] = &tally{}
	}
	var cells, same int
	for _, p := range pos {
		k := [2]int{int(p[0]), int(p[1])}
		bc, ours := w.chunks[k], got[k]
		for cell := range chunkCells - 256 { // Y 319 holds the study's loading marker.
			if bc.masked(cell) {
				continue
			}
			real, mine := w.names[bc.ids[cell]], ours.name(cell)
			cells++
			if real == mine {
				same++
			}
			for _, n := range names {
				a, b := real == n, mine == n
				if a {
					all[n].bds++
				}
				if b {
					all[n].ours++
				}
				if a && b {
					all[n].both++
				}
			}
		}
	}
	t.Logf("mangrove_swamp_20: %d chunks, same %d/%d = %.4f%%, wrong %d", len(pos), same, cells,
		100*float64(same)/float64(max(cells, 1)), cells-same)
	for _, n := range names {
		c := all[n]
		t.Logf("  %-20s BDS %7d ours %7d both %7d: recall %6.2f%%, precision %6.2f%%", n, c.bds, c.ours, c.both,
			100*float64(c.both)/float64(max(c.bds, 1)), 100*float64(c.both)/float64(max(c.ours, 1)))
	}
}
