package structures

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestPieceTablesResolve: every block state the piece tables name is a registered state, and every op is
// one the runners know. The loaders panic on any of these (pieceTables.check, nfTables, omTables); this
// test names each failure instead, over every table in piecedata/.
func TestPieceTablesResolve(t *testing.T) {
	d := pieceData()
	check := func(where, s string) {
		if specState(s) == noBlock {
			t.Errorf("%s: state %q is not registered", where, s)
		}
	}
	for dir, m := range d.facings {
		for k, s := range m {
			check("facing "+dir+" "+k, s)
		}
	}
	for _, tbl := range []map[string]string{d.misc, d.msStates, d.shStates} {
		for k, s := range tbl {
			check(k, s)
		}
	}
	facingKey := func(b string) bool {
		return b == "TORCH" || b == "LADDER" || strings.HasPrefix(b, "STAIRS:") || strings.HasPrefix(b, "DOOR:") ||
			strings.HasPrefix(b, "BUTTON") || strings.HasPrefix(b, "FRAME")
	}
	ops := func(where string, list []pieceOp, own []string) {
		known := map[string]bool{}
		for _, o := range append(append([]string(nil), commonOps...), own...) {
			known[o] = true
		}
		for _, op := range list {
			if !known[op.Op] {
				t.Errorf("%s: unknown op %q", where, op.Op)
			}
			for _, b := range []string{op.Block, op.Inner, op.IfTrue, op.IfFalse} {
				if b == "" {
					continue
				}
				if facingKey(b) {
					for _, dir := range facingNames {
						if _, ok := d.facings[dir][strings.Replace(b, "TORCH", "TORCH (wall torch pointing this way)", 1)]; !ok {
							t.Errorf("%s: facing block %q not in the %s facing table", where, b, dir)
						}
					}
					continue
				}
				check(where, b)
			}
		}
	}
	ops("mineshaft stairs", d.msStairs, nil)
	for k, l := range d.shPieces {
		ops("stronghold "+k, l, shOps)
	}
	ops("pyramid", d.pyramid, pyramidOps)
	ops("jungle temple", d.jungle, jungleOps)
	ops("witch hut", d.hut, hutOps)
	for k, cells := range d.shDoors {
		for _, c := range cells {
			if c.Facing == "" {
				check("door "+k, c.Block)
			}
		}
	}
	for _, f := range []int32{2, 4, 6} {
		check("cauldron", cauldronState(f))
	}
	if len(d.shPieces) != 21 {
		t.Errorf("stronghold op lists: %d, want 21", len(d.shPieces))
	}

	// The fortress table: every palette entry ("name k=v ..." or nether brick stairs with a facing) and
	// every layer key.
	var fortress []nfRaw
	readTable("fortress_pieces.json", &fortress)
	for _, r := range fortress {
		for _, n := range r.Palette {
			if name, f, ok := strings.Cut(n, " facing="); ok {
				if _, known := weirdo[f]; !known || name != "nether_brick_stairs" {
					t.Errorf("fortress %s: stairs %q", r.Kind, n)
				}
			} else if paletteState(n) == noBlock {
				t.Errorf("fortress %s: block %q is not registered", r.Kind, n)
			}
		}
		for _, layer := range r.Layers {
			for _, row := range layer {
				for _, ch := range row {
					if i := strings.IndexRune(nfKeys, ch); ch != '.' && (i < 0 || i >= len(r.Palette)) {
						t.Errorf("fortress %s: layer key %q outside its palette", r.Kind, ch)
					}
				}
			}
		}
	}
	// The monument tables: every block an op writes.
	var building []omRawOp
	readTable("monument_building.json", &building)
	var rooms map[string][]omRawOp
	readTable("monument_rooms.json", &rooms)
	rooms["building"] = building
	for k, list := range rooms {
		for _, op := range list {
			if op.Block != "" {
				check("monument "+k, op.Block)
			}
		}
	}
	// Every table in the folder is one of the above.
	entries, err := pieceFiles.ReadDir("piecedata")
	if err != nil {
		t.Fatal(err)
	}
	read := map[string]bool{"facing_states.json": true, "misc_states.json": true, "mineshaft_states.json": true,
		"mineshaft_stairs.json": true, "stronghold_pieces.json": true, "stronghold_doors.json": true,
		"stronghold_states.json": true, "desert_pyramid.json": true, "jungle_temple.json": true, "witch_hut.json": true,
		"fortress_pieces.json": true, "monument_building.json": true, "monument_rooms.json": true}
	for _, e := range entries {
		if !read[e.Name()] {
			t.Errorf("piecedata/%s is not checked here", e.Name())
		}
	}
	nfTables()
	omTables()
}

// TestFrameAndOrientBox: the frame maps and oriented boxes agree: a piece placed with orientBox going d
// has its local (0, 0, 0) at the foot's side and its local +z along d.
func TestFrameAndOrientBox(t *testing.T) {
	for _, d := range []orient{oS, oW, oN, oE} {
		b := orientBox(100, 20, 200, 0, 0, 0, 3, 3, 10, d)
		p := opiece{b, d}
		x, _, z := p.world(0, 0, 0)
		if x != 100 || z != 200 {
			t.Errorf("%v: local origin (%d, %d), want the foot (100, 200)", d, x, z)
		}
		x1, _, z1 := p.world(2, 2, 9)
		if !b.inside([3]int{x1, 22, z1}) {
			t.Errorf("%v: local far corner (%d, %d) outside %v", d, x1, z1, b)
		}
		x2, _, z2 := p.world(0, 0, 9)
		if got := worldFacing(x2-x, z2-z); got != p.turn(fN) || got != [4]facing{fS, fW, fN, fE}[d] {
			t.Errorf("%v: local +z runs %v, turn says %v", d, got, p.turn(fN))
		}
	}
}

func readSpecLayout(t *testing.T, name string) []map[string]any {
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// normal turns a value into its JSON form, so it compares with a decoded table entry.
func normal(t *testing.T, v any) map[string]any {
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	json.Unmarshal(raw, &out)
	return out
}

var orientNames = [5]string{"S", "W", "N", "E", ""}

func compareLayout(t *testing.T, name string, ours []map[string]any, want []map[string]any) {
	if len(ours) != len(want) {
		t.Errorf("%s: %d pieces, want %d", name, len(ours), len(want))
	}
	bad := 0
	for i := range min(len(ours), len(want)) {
		if !reflect.DeepEqual(ours[i], want[i]) {
			bad++
			if bad <= 5 {
				t.Errorf("%s piece %d:\n ours %v\n want %v", name, i, ours[i], want[i])
			}
		}
	}
	if bad == 0 && len(ours) == len(want) {
		t.Logf("%s: %d of %d pieces equal the spec's layout", name, len(want), len(want))
	}
}

// TestMineshaftLayoutMatchesSpec: the two reference mineshafts, piece for piece.
func TestMineshaftLayoutMatchesSpec(t *testing.T) {
	for _, c := range []struct {
		seed   int64
		cx, cz int
		file   string
	}{{5, -12, -52, "mineshaft-seed5--12--52.json"}, {100, 52, 101, "mineshaft-seed100-52-101.json"}} {
		_, s := worldFor(t, c.seed)
		m := s.mineshaftAtStart(c.cx, c.cz)
		kinds := [...]string{"room", "corridor", "crossing", "stairs"}
		var ours []map[string]any
		for i, p := range m.pieces {
			e := map[string]any{"box": p.b, "depth": p.depth, "i": i, "kind": kinds[p.kind]}
			switch p.kind {
			case msRoom:
				e["entrances"] = m.entrances
			case msCorridor:
				e["dir"], e["rails"], e["sections"], e["spider"] = orientNames[p.dir], p.rails, p.sections, p.spider
			case msCrossing:
				e["entered"] = orientNames[p.dir]
			case msStairs:
				e["dir"] = orientNames[p.dir]
			}
			ours = append(ours, normal(t, e))
		}
		compareLayout(t, c.file, ours, readSpecLayout(t, c.file))
	}
}

// TestStrongholdLayoutMatchesSpec: seed 100's stronghold at (53,100), piece for piece.
func TestStrongholdLayoutMatchesSpec(t *testing.T) {
	_, s := worldFor(t, 100)
	h := s.strongholdAtStart(53, 100)
	flagNames := map[int][]string{shCorridor: {"side_exit_x0", "side_exit_x1"},
		shFiveWay: {"opening_x0_low", "opening_x0_high", "opening_x1_low", "opening_x1_high"}}
	var ours []map[string]any
	for i, p := range h.pieces {
		e := map[string]any{"box": p.b, "depth": p.depth, "i": i, "kind": shKindNames[p.kind], "orientation": orientNames[p.o]}
		if p.kind != shPortalRoom && p.kind != shStub {
			e["door"] = shDoorNames[p.door]
		}
		for j, n := range flagNames[p.kind] {
			e[n] = p.flags[j]
		}
		switch p.kind {
		case shJunction:
			e["type"] = p.jtype
		case shLibrary:
			e["tall"] = p.tall
		case shStub:
			e["length"] = p.length
		}
		ours = append(ours, normal(t, e))
	}
	compareLayout(t, "stronghold-seed100-53-100.json", ours, readSpecLayout(t, "stronghold-seed100-53-100.json"))
}
