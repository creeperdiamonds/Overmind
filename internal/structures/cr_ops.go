package structures

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/df-mc/dragonfly/server/world/chunk"
)

// The piece tables of the code-built structures (crdata/*.json: facts about the pieces' blocks, in local
// coordinates, from the clean-room spec; embedded as crFiles in cr_common.go) and the runner of their op
// lists.

// crOp is one op of a piece's op list. Which fields are set depends on Op.
type crOp struct {
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

// crDoorCell is one cell of a stronghold door.
type crDoorCell struct {
	At     [3]int `json:"at"`
	Block  string `json:"block"`
	Facing string `json:"facing"`
}

type crTables struct {
	err error
	// facings: world direction ("north", ...) -> key ("LADDER", "STAIRS:stone_stairs", ...) -> state;
	// "rails" holds the two rail states.
	facings  map[string]map[string]string
	misc     map[string]string
	msStates map[string]string
	msStairs []crOp
	shPieces map[string][]crOp
	shDoors  map[string][]crDoorCell
	shStates map[string]string
	pyramid  []crOp
	jungle   []crOp
	hut      []crOp
}

var crData = sync.OnceValue(func() *crTables {
	t := &crTables{}
	read := func(name string, v any) {
		if t.err != nil {
			return
		}
		raw, err := crFiles.ReadFile("crdata/" + name)
		if err == nil {
			err = json.Unmarshal(raw, v)
		}
		if err != nil {
			t.err = fmt.Errorf("structures: piece table %s: %w", name, err)
		}
	}
	read("facing_states.json", &t.facings)
	read("misc_states.json", &t.misc)
	read("mineshaft_states.json", &t.msStates)
	var stairs map[string][]crOp
	read("mineshaft_stairs.json", &stairs)
	t.msStairs = stairs["stairs"]
	read("stronghold_pieces.json", &t.shPieces)
	var doors map[string]json.RawMessage
	read("stronghold_doors.json", &doors)
	t.shDoors = map[string][]crDoorCell{}
	for k, raw := range doors {
		if k == "_about" {
			continue
		}
		var cells []crDoorCell
		if err := json.Unmarshal(raw, &cells); err != nil && t.err == nil {
			t.err = fmt.Errorf("structures: stronghold door %s: %w", k, err)
		}
		t.shDoors[k] = cells
	}
	read("stronghold_states.json", &t.shStates)
	read("desert_pyramid.json", &t.pyramid)
	read("jungle_temple.json", &t.jungle)
	read("witch_hut.json", &t.hut)
	return t
})

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
	st, ok := crData().facings[facingName(p.turn(f))][key]
	if !ok {
		return noBlock
	}
	return specState(st)
}

// railRID is a rail along world z (true) or world x.
func railRID(alongZ bool) uint32 {
	k := "rail along world x (east-west)"
	if alongZ {
		k = "rail along world z (north-south)"
	}
	return specState(crData().facings["rails"][k])
}

// runOps runs an op list in c's piece frame. flag answers the ops' `when` fields; special runs the
// structure's own ops and reports whether it knew the op.
func runOps(c *ctx, ops []crOp, flag func(string) bool, special func(c *ctx, op *crOp) bool) {
	if crData().err != nil {
		return
	}
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
			// Every op of the embedded tables is known (TestPieceTablesResolve); an unknown one does nothing.
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
	for ; wy > minY; wy-- {
		if !c.w.in(wx, wy, wz) {
			return
		}
		cur := c.w.mainAt(wx, wy, wz)
		if cur != air && !ridHas(cur, flagLiquid) {
			if ridName(cur) == "grass_block" {
				c.w.put(wx, wy, wz, specState(crData().misc["dirt"]))
			}
			return
		}
		c.w.put(wx, wy, wz, rid)
	}
}

// lootEntity is a container's block entity with a loot table and seed.
func lootEntity(id string, x, y, z int, table string, seed int32) chunk.BlockEntity {
	return chunk.BlockEntity{Pos: [3]int{x, y, z}, Data: map[string]any{"id": id, "LootTable": table, "LootTableSeed": seed}}
}
