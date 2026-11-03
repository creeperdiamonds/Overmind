package worldgen

import (
	"compress/gzip"
	"encoding/csv"
	"os"
	"path/filepath"
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
	for _, r := range rows[1:] {
		name := strings.TrimPrefix(r[0], "minecraft:")
		s, ok := known[name]
		if !ok {
			continue
		}
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
	if checked == 0 {
		t.Fatal("no state of ours is in the list")
	}
	t.Logf("%d of our states checked, %d flags differ", checked, bad)
}
