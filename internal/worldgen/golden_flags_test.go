package worldgen

import (
	"compress/gzip"
	"encoding/csv"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestGoldenStateFlags checks our flags for every state the generator uses against the study's list
// of block flags from the game.
func TestGoldenStateFlags(t *testing.T) {
	dir := goldenFeaturesDir(t)
	f, err := os.Open(filepath.Join(dir, "block_props.csv.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(z).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 2 {
		t.Fatal("block_props holds no rows")
	}
	col := map[string]int{}
	for i, n := range rows[0] {
		col[n] = i
	}
	flags := []struct {
		name string
		bit  uint16
	}{{"is_air", fAir}, {"blocks_motion", fMotion}, {"is_solid", fSolid}, {"liquid", fLiquid}, {"solid_render", fSolidRender},
		{"sturdy_up", fSturdyUp}, {"sturdy_down", fSturdyDown}, {"sturdy_side", fSturdySide}, {"replaceable", fReplaceable}}
	stateLock.Lock()
	known := map[string]bs{}
	for n, s := range stateIndex {
		known[n] = s
	}
	stateLock.Unlock()
	checked, bad := 0, 0
	inList := map[string]bool{}
	for _, r := range rows[1:] {
		name := strings.TrimPrefix(r[0], "minecraft:")
		s, ok := known[name]
		if !ok {
			continue
		}
		inList[name] = true
		checked++
		for _, fl := range flags {
			want := r[col[fl.name]] == "1"
			if s.has(fl.bit) != want {
				bad++
				if bad <= 10 {
					t.Errorf("%s: %s is %v here, %v in the game", name, fl.name, s.has(fl.bit), want)
				}
			}
		}
		source := r[col["fluid_source"]] == "1"
		empty := r[col["fluid_empty"]] == "1"
		if s.hasFluid() == empty || (s.has(fSource) && !empty) != source {
			bad++
			if bad <= 10 {
				t.Errorf("%s: fluid flags differ", name)
			}
		}
	}
	var unchecked []string
	for n := range known {
		if !inList[n] {
			unchecked = append(unchecked, n)
		}
	}
	slices.Sort(unchecked)
	t.Logf("%d of our %d states checked, %d flags differ; %d not in the list: %s", checked, len(known), bad, len(unchecked), strings.Join(unchecked[:min(len(unchecked), 30)], " "))
	// The list covers the states the fixtures' chunks hold, so most of ours should be in it.
	if checked < 400 {
		t.Errorf("only %d of our states are in the game's list", checked)
	}
}
