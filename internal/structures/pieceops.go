package structures

import (
	"embed"
	"encoding/json"
	"fmt"
	"sync"
)

// The piece tables of the code-built structures (piecedata/*.json: facts about the pieces' blocks, in
// local coordinates, from the clean-room spec; nothing in them is code) and the runner of their op lists.
// The shared frame, writes and block lookups they use are pieces.go.
//
// The tables are embedded at build time, so one that is missing or malformed, or names a block state the
// registry does not have, is a broken build: every loader here panics with what is wrong.

//go:embed piecedata/*.json
var pieceFiles embed.FS

// pieceTable reads one embedded table.
func pieceTable(name string) []byte {
	b, err := pieceFiles.ReadFile("piecedata/" + name)
	if err != nil {
		panic(fmt.Sprintf("structures: piece table %s: %v", name, err))
	}
	return b
}

// readTable decodes one embedded table into v.
func readTable(name string, v any) {
	if err := json.Unmarshal(pieceTable(name), v); err != nil {
		panic(fmt.Sprintf("structures: piece table %s: %v", name, err))
	}
}

// pieceOp is one op of a piece's op list. Which fields are set depends on Op.
type pieceOp struct {
	Op           string   `json:"op"`
	Block        string   `json:"block"`
	Inner        string   `json:"inner"`
	From         [3]int   `json:"from"`
	To           [3]int   `json:"to"`
	At           [3]int   `json:"at"`
	P            float32  `json:"p"`
	Facing       string   `json:"facing"`
	When         string   `json:"when"`
	OnlyNonAir   bool     `json:"only_nonair"`
	InteriorOnly bool     `json:"interior_only"`
	Type         string   `json:"type"`
	Loot         string   `json:"loot"`
	Cells        [][3]int `json:"cells"`
	IfTrue       string   `json:"if_true"`
	IfFalse      string   `json:"if_false"`
	X            [2]int   `json:"x"`
	Z            [2]int   `json:"z"`
	Y            int      `json:"y"`
	EntityID     string   `json:"entity_id"`
	Plant        string   `json:"plant"`
	Centre       [3]int   `json:"cellar_centre"`
}

// doorCell is one cell of a stronghold door.
type doorCell struct {
	At     [3]int `json:"at"`
	Block  string `json:"block"`
	Facing string `json:"facing"`
}

type pieceTables struct {
	// facings: world direction ("north", ...) -> key ("LADDER", "STAIRS:stone_stairs", ...) -> state;
	// "rails" holds the two rail states.
	facings  map[string]map[string]string
	misc     map[string]string
	msStates map[string]string
	msStairs []pieceOp
	shPieces map[string][]pieceOp
	shDoors  map[string][]doorCell
	shStates map[string]string
	pyramid  []pieceOp
	jungle   []pieceOp
	hut      []pieceOp
}

var pieceData = sync.OnceValue(func() *pieceTables {
	t := &pieceTables{}
	readTable("facing_states.json", &t.facings)
	readTable("misc_states.json", &t.misc)
	readTable("mineshaft_states.json", &t.msStates)
	var stairs map[string][]pieceOp
	readTable("mineshaft_stairs.json", &stairs)
	t.msStairs = stairs["stairs"]
	readTable("stronghold_pieces.json", &t.shPieces)
	var doors map[string]json.RawMessage
	readTable("stronghold_doors.json", &doors)
	t.shDoors = map[string][]doorCell{}
	for k, raw := range doors {
		if k == "_about" {
			continue
		}
		var cells []doorCell
		if err := json.Unmarshal(raw, &cells); err != nil {
			panic(fmt.Sprintf("structures: stronghold door %s: %v", k, err))
		}
		t.shDoors[k] = cells
	}
	readTable("stronghold_states.json", &t.shStates)
	readTable("desert_pyramid.json", &t.pyramid)
	readTable("jungle_temple.json", &t.jungle)
	readTable("witch_hut.json", &t.hut)
	t.check()
	return t
})

// The ops every list may use (runOps runs them), and each list's own ops (its structure's special).
var (
	commonOps  = []string{"fill", "set", "chance", "chance_box", "column_down", "column_down_each"}
	shOps      = []string{"stone_box", "door", "chest", "portal_frames"}
	pyramidOps = []string{"chests_in_order", "cellar_variant", "set_by_variant", "archaeology_begin", "listed_sand", "roof_choice", "suspicious_pass"}
	jungleOps  = []string{"mossy_box", "container"}
	hutOps     = []string{"flower_pot_entity", "cauldron"}
)

// The keys the code itself reads from the state and facing tables.
var (
	miscKeys    = []string{"chest", "suspicious_sand", "dirt"}
	msStateKeys = []string{"cobweb", "chain", "spawner", "planks normal", "planks mesa", "log normal", "log mesa", "fence normal", "fence mesa"}
	shStateKeys = []string{"cracked", "mossy", "infested", "stone_bricks", "end_portal", "spawner"}
	facingKeys  = []string{"TORCH (wall torch pointing this way)", "FRAME eye=false", "FRAME eye=true"}
	railKeys    = []string{"rail along world x (east-west)", "rail along world z (north-south)"}
)

// cauldronState is the witch hut's water cauldron at a fill level (2, 4 or 6).
func cauldronState(fill int32) string {
	return fmt.Sprintf("cauldron[cauldron_liquid=water,fill_level=%d]", fill)
}

// check panics unless every state the tables name is registered, every key the code reads is there,
// every op is one its list's runner knows, and the pyramid's archaeology ops come in an order its runner
// can follow.
func (t *pieceTables) check() {
	fail := func(format string, a ...any) { panic("structures: piece tables: " + fmt.Sprintf(format, a...)) }
	for _, dir := range []string{"north", "south", "west", "east", "rails"} {
		if len(t.facings[dir]) == 0 {
			fail("the facing table has no %q", dir)
		}
		for _, s := range t.facings[dir] {
			mustState(s)
		}
	}
	facingKey := func(where, key string) {
		if key == "TORCH" {
			key = "TORCH (wall torch pointing this way)"
		}
		for _, dir := range facingNames {
			if _, ok := t.facings[dir][key]; !ok {
				fail("%s: facing key %q is not in the %s table", where, key, dir)
			}
		}
	}
	for _, k := range facingKeys {
		facingKey("code", k)
	}
	for _, k := range railKeys {
		if _, ok := t.facings["rails"][k]; !ok {
			fail("no rail %q", k)
		}
	}
	for _, tbl := range []struct {
		name string
		m    map[string]string
		keys []string
	}{{"misc", t.misc, miscKeys}, {"mineshaft", t.msStates, msStateKeys}, {"stronghold", t.shStates, shStateKeys}} {
		for _, k := range tbl.keys {
			if _, ok := tbl.m[k]; !ok {
				fail("%s states: no %q", tbl.name, k)
			}
		}
		for _, s := range tbl.m {
			mustState(s)
		}
	}
	for _, f := range []int32{2, 4, 6} {
		mustState(cauldronState(f))
	}
	ops := func(where string, list []pieceOp, own []string) {
		known := map[string]bool{}
		for _, o := range append(append([]string(nil), commonOps...), own...) {
			known[o] = true
		}
		for _, op := range list {
			if !known[op.Op] {
				fail("%s: unknown op %q", where, op.Op)
			}
			if op.Op == "door" && op.Type != "own" {
				if _, ok := t.shDoors[op.Type]; !ok {
					fail("%s: no door %q", where, op.Type)
				}
			}
			if _, ok := localFacing(op.Facing); ok {
				facingKey(where, op.Block)
				if op.Inner != "" {
					facingKey(where, op.Inner)
				}
				continue
			}
			for _, s := range []string{op.Block, op.Inner, op.IfTrue, op.IfFalse} {
				if s != "" {
					mustState(s)
				}
			}
		}
	}
	ops("mineshaft stairs", t.msStairs, nil)
	for k, l := range t.shPieces {
		ops("stronghold "+k, l, shOps)
	}
	ops("desert pyramid", t.pyramid, pyramidOps)
	ops("jungle temple", t.jungle, jungleOps)
	ops("witch hut", t.hut, hutOps)
	for _, n := range shDoorNames {
		if _, ok := t.shDoors[n]; !ok && n != "" {
			fail("no stronghold door %q", n)
		}
	}
	for k, cells := range t.shDoors {
		for _, c := range cells {
			if _, ok := localFacing(c.Facing); ok {
				facingKey("door "+k, c.Block)
			} else {
				mustState(c.Block)
			}
		}
	}
	// The pyramid's archaeology: its random exists from archaeology_begin on, and the suspicious pass
	// picks from the sand listed since.
	begun, listed := false, 0
	for _, op := range t.pyramid {
		switch op.Op {
		case "archaeology_begin":
			begun, listed = true, 0
		case "listed_sand", "roof_choice", "suspicious_pass":
			if !begun {
				fail("desert pyramid: %s before archaeology_begin", op.Op)
			}
			if op.Op == "listed_sand" {
				listed++
			}
			if op.Op == "suspicious_pass" && listed == 0 {
				fail("desert pyramid: suspicious_pass with no listed_sand before it")
			}
		}
	}
}

// localFacing reads an op's local direction.
func localFacing(s string) (facing, bool) {
	switch s {
	case "+z":
		return fN, true
	case "-z":
		return fS, true
	case "-x":
		return fW, true
	case "+x":
		return fE, true
	}
	return 0, false
}

// blockOf is the state an op's block names in piece p: a state string, or a key of the facing table
// turned by the piece when the op gives a local facing.
func blockOf(block, face string, p *opiece) uint32 {
	f, ok := localFacing(face)
	if !ok {
		return specState(block)
	}
	key := block
	if key == "TORCH" {
		key = "TORCH (wall torch pointing this way)"
	}
	return specState(pieceData().facings[facingName(p.turn(f))][key])
}

// railRID is a rail along world z (true) or world x.
func railRID(alongZ bool) uint32 {
	k := railKeys[0]
	if alongZ {
		k = railKeys[1]
	}
	return specState(pieceData().facings["rails"][k])
}

// runOps runs an op list in c's piece frame. flag answers the ops' `when` fields; special runs the
// structure's own ops (pieceTables.check has made sure every op of a list is one its runner knows).
func runOps(c *ctx, ops []pieceOp, flag func(string) bool, special func(c *ctx, op *pieceOp)) {
	for i := range ops {
		op := &ops[i]
		if op.When != "" && (flag == nil || !flag(op.When)) {
			continue
		}
		switch op.Op {
		case "fill":
			edge := blockOf(op.Block, op.Facing, c.p)
			fill := edge
			if op.Inner != "" {
				fill = blockOf(op.Inner, op.Facing, c.p)
			}
			c.box(op.From[0], op.From[1], op.From[2], op.To[0], op.To[1], op.To[2], edge, fill, op.OnlyNonAir)
		case "set":
			c.set(op.At[0], op.At[1], op.At[2], blockOf(op.Block, op.Facing, c.p))
		case "chance":
			c.maybe(op.P, op.At[0], op.At[1], op.At[2], blockOf(op.Block, op.Facing, c.p))
		case "chance_box":
			rid := blockOf(op.Block, op.Facing, c.p)
			c.maybeBox(op.P, op.From[0], op.From[1], op.From[2], op.To[0], op.To[1], op.To[2], rid, rid, op.OnlyNonAir, op.InteriorOnly)
		case "column_down":
			c.columnDown(op.At[0], op.At[1], op.At[2], blockOf(op.Block, op.Facing, c.p))
		case "column_down_each":
			rid := blockOf(op.Block, op.Facing, c.p)
			for x := op.X[0]; x <= op.X[1]; x++ {
				for z := op.Z[0]; z <= op.Z[1]; z++ {
					c.columnDown(x, op.Y, z, rid)
				}
			}
		default:
			if special != nil {
				special(c, op)
			}
		}
	}
}

// columnDown fills down from a local cell through air and liquid (main block), stopping at the chunk's
// edge or at the first other block, which becomes dirt if it is grass.
func (c *ctx) columnDown(x, y, z int, rid uint32) {
	wx, wy, wz := c.p.world(x, y, z)
	air := airRID()
	for ; wy > c.w.lo; wy-- {
		if !c.w.in(wx, wy, wz) {
			return
		}
		cur := c.w.mainAt(wx, wy, wz)
		if cur != air && !ridHas(cur, flagLiquid) {
			if ridName(cur) == "grass_block" {
				c.w.put(wx, wy, wz, specState(pieceData().misc["dirt"]))
			}
			return
		}
		c.w.put(wx, wy, wz, rid)
	}
}
