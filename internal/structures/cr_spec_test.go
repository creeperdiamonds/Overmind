package structures

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The clean-room specification's acceptance dumps (outside the repo; OVERMIND_CLEANROOM_SPEC names the
// folder holding monument/, fortress/ and mansion/) list the assembled acceptance starts piece by
// piece. These tests compare our assembly with them; they are development aids and skip without it.

func cleanroomSpec(t *testing.T, file string, into any) {
	dir := os.Getenv("OVERMIND_CLEANROOM_SPEC")
	if dir == "" {
		t.Skip("OVERMIND_CLEANROOM_SPEC is not set")
	}
	raw, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatal(err)
	}
}

var crOrientNames = map[orient]string{oN: "N", oS: "S", oW: "W", oE: "E"}

func TestCleanroomMonumentAssembly(t *testing.T) {
	var want struct {
		Building    box    `json:"building_box"`
		Orientation string `json:"orientation"`
		StartSeed   uint32 `json:"start_seed"`
		Children    []struct {
			Kind   string `json:"kind"`
			Box    box    `json:"box"`
			Design int    `json:"design"`
		} `json:"children"`
		Rooms []struct {
			Index int    `json:"index"`
			Open  string `json:"open"`
		} `json:"rooms_after_closing"`
	}
	cleanroomSpec(t, "monument/acceptance-seed32--12-6.json", &want)
	_, s := worldFor(t, 32)
	if got := s.monumentStartSeed(-12, 6); got != want.StartSeed {
		t.Errorf("start seed %#x, want %#x", got, want.StartSeed)
	}
	m := s.monumentAtStart(-12, 6)
	if m.building.b != want.Building || crOrientNames[m.building.o] != want.Orientation {
		t.Errorf("building %v %s, want %v %s", m.building.b, crOrientNames[m.building.o], want.Building, want.Orientation)
	}
	bad := 0
	for i := range max(len(m.children), len(want.Children)) {
		got, exp := "-", "-"
		if i < len(m.children) {
			c := m.children[i]
			got = fmt.Sprintf("%s %v %d", c.kind, c.b, c.design)
		}
		if i < len(want.Children) {
			c := want.Children[i]
			exp = fmt.Sprintf("%s %v %d", c.Kind, c.Box, c.Design)
		}
		if got != exp {
			bad++
			t.Errorf("child %d: ours %s, spec %s", i, got, exp)
		}
	}
	faces := "DUNSWE"
	for _, r := range want.Rooms {
		if r.Index >= 75 {
			continue
		}
		c := m.grid[r.Index]
		if c == nil {
			t.Errorf("room %d missing", r.Index)
			continue
		}
		got := ""
		for f := range 6 {
			if c.open[f] {
				got += faces[f : f+1]
			}
		}
		if sortString(got) != sortString(r.Open) {
			bad++
			t.Errorf("room %d open %q, spec %q", r.Index, got, r.Open)
		}
	}
	t.Logf("monument: %d children (spec %d), %d differences", len(m.children), len(want.Children), bad)
}

func sortString(s string) string {
	b := []byte(s)
	for i := range b {
		for j := i + 1; j < len(b); j++ {
			if b[j] < b[i] {
				b[i], b[j] = b[j], b[i]
			}
		}
	}
	return string(b)
}

func TestCleanroomFortressAssembly(t *testing.T) {
	var want struct {
		Bounds box `json:"bounds"`
		Pieces []struct {
			Kind   string  `json:"kind"`
			Box    box     `json:"box"`
			Orient string  `json:"orientation"`
			Depth  int     `json:"depth"`
			Chest  bool    `json:"chest"`
			Filler *uint32 `json:"filler_seed"`
		} `json:"pieces"`
	}
	cleanroomSpec(t, "fortress/acceptance-seed100-4-19.json", &want)
	_, s := netherFor(t, 100)
	st, ok := s.StartIn(Fortress, 4, 19)
	if !ok {
		t.Fatal("(4,19) is not a fortress start")
	}
	f := s.fortressAtStart(st)
	bad := 0
	for i := range max(len(f.pieces), len(want.Pieces)) {
		got, exp := "-", "-"
		if i < len(f.pieces) {
			p := f.pieces[i]
			got = fmt.Sprintf("%s %v %s %d %v %d", nfKindNames[p.kind], p.b, crOrientNames[p.o], p.depth, p.chest, p.filler)
		}
		if i < len(want.Pieces) {
			p := want.Pieces[i]
			fs := uint32(0)
			if p.Filler != nil {
				fs = *p.Filler
			}
			exp = fmt.Sprintf("%s %v %s %d %v %d", p.Kind, p.Box, p.Orient, p.Depth, p.Chest, fs)
		}
		if got != exp {
			if bad < 10 {
				t.Errorf("piece %d: ours %s, spec %s", i, got, exp)
			}
			bad++
		}
	}
	t.Logf("fortress: %d pieces (spec %d), bounds %v (spec %v), %d differ", len(f.pieces), len(want.Pieces), boundsOf(pieceBoxes(f.pieces)), want.Bounds, bad)
}

func TestCleanroomMansionAssembly(t *testing.T) {
	var want struct {
		Draws  int      `json:"draws_after_rotation_total"`
		Origin [3]int   `json:"origin"`
		Grid   []string `json:"grid_rows_y_outer"`
		Third  []string `json:"third_floor_grid"`
		Pieces []struct {
			Name   string `json:"name"`
			Origin [3]int `json:"origin"`
			Rot    int    `json:"rotation"`
			Mirror int    `json:"mirror"`
			Box    box    `json:"box"`
		} `json:"pieces"`
	}
	cleanroomSpec(t, "mansion/acceptance-seed987654321-105--292.json", &want)
	_, s := worldFor(t, 987654321)
	m := s.mansionAt(105, -292)
	if m == nil {
		t.Fatalf("no mansion (problems %v)", s.data.Problems)
	}
	if m.draws != want.Draws {
		t.Errorf("%d draws after the rotation, spec %d", m.draws, want.Draws)
	}
	bad := 0
	for i := range max(len(m.pieces), len(want.Pieces)) {
		got, exp := "-", "-"
		if i < len(m.pieces) {
			p := m.pieces[i]
			got = fmt.Sprintf("%s %v %d %d %v", p.name, p.pos, p.rot, p.mirror, p.box)
		}
		if i < len(want.Pieces) {
			p := want.Pieces[i]
			exp = fmt.Sprintf("%s %v %d %d %v", p.Name, p.Origin, p.Rot, p.Mirror, p.Box)
		}
		if got != exp {
			if bad < 10 {
				t.Errorf("piece %d: ours %s, spec %s", i, got, exp)
			}
			bad++
		}
	}
	t.Logf("mansion: %d pieces (spec %d), %d draws, %d differ; spec grid %s", len(m.pieces), len(want.Pieces), m.draws, bad, strings.Join(want.Grid, " / "))
}
