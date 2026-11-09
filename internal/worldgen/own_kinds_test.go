package worldgen

import (
	"context"
	"math"
	"sort"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Every state the generator can write, in either profile, is a block a player can break as in the game
// (round 19: the owner's bushes could not be broken). States that map to no block are lookup keys
// (tags, matches): a placed one panics under test in every generating test (unknownState).
func TestEveryGeneratedBlockBreaks(t *testing.T) {
	NewWithProfile(1, ProfileOverjava)
	if bd, _ := testData(); bd != nil {
		g := NewWithData(1, ProfileBedrock, bd)
		g.Nether()
		g.End()
	} else {
		NewWithProfile(1, ProfileBedrock)
	}
	stateLock.Lock()
	names := make([]string, 0, len(stateIndex))
	for n := range stateIndex {
		names = append(names, n)
	}
	stateLock.Unlock()
	sort.Strings(names)
	kinds := map[string]bool{}
	for _, n := range names {
		var b world.Block
		func() {
			defer func() { _ = recover() }()
			b = dragonflyBlock(n)
		}()
		if b == nil {
			continue
		}
		en, _ := b.EncodeBlock()
		kinds[en] = true
		if why := CheckBlockBreaks(b); why != "" {
			t.Errorf("%s: %s", n, why)
		}
	}
	t.Logf("%d states, %d block kinds checked", len(names), len(kinds))

	// The owner's report: the plants of 1.21.5 and later are Dragonfly fork types that break at once by
	// hand, as the client expects, and the dappled forest's blocks are written (they were air).
	hand := item.NewStack(item.Stick{}, 1)
	for _, n := range []string{"bush", "firefly_bush", "short_dry_grass", "tall_dry_grass", "cactus_flower",
		"leaf_litter[facing=north,segment_amount=1]", "wildflowers[facing=east,flower_amount=3]", "red_shrub"} {
		b := dragonflyBlock(stateOf(n).name())
		if _, own := b.(OwnBlock); own {
			t.Errorf("%s is still an own block", n)
		}
		if d := block.BreakDuration(b, hand, block.BreakContext{}); d != 0 {
			t.Errorf("%s takes %v by hand, want at once", n, d)
		}
	}
	if b := dragonflyBlock("poplar_log[axis=y]"); b != (block.Log{Wood: block.PoplarWood(), Axis: cube.Y}) {
		t.Errorf("poplar_log is %#v", b)
	}

	// Our own blocks break in the game's time: moss by hand 0.15 s, sculk catalyst by hand 4.5 s.
	for n, want := range map[string]float64{"moss_block": 0.15, "sculk_catalyst[bloom=false]": 4.5, "crimson_fungus": 0} {
		b := dragonflyBlock(n)
		if got := block.BreakDuration(b, hand, block.BreakContext{}).Seconds(); math.Abs(got-want) > 0.051 {
			t.Errorf("%s takes %.2f s by hand, want %.2f", n, got, want)
		}
	}
}

// No state of a block with a kind is left as Dragonfly's unknown block, and a placeable kind has an item.
func TestStandInsCoverTheKinds(t *testing.T) {
	registerOwnBlocks()
	world.DefaultBlockRegistry.Finalize()
	for _, b := range world.Blocks() {
		name, _ := b.EncodeBlock()
		if _, ok := ownKindOf(name); !ok {
			continue
		}
		if why := CheckBlockBreaks(b); why != "" {
			t.Errorf("%s", why)
		}
	}
	for _, n := range []string{"moss_block", "pale_moss_block", "mangrove_roots", "sculk", "bell", "target"} {
		k, _ := ownKindOf(n)
		if _, ok := world.ItemByName("minecraft:"+n, 0); !ok && k.placeable {
			t.Errorf("%s has no item", n)
		}
	}
}

// Snow layers and seagrass have items (round 19): seagrass goes into a water source on a solid floor
// and drops itself to shears; a snow layer item places a layer.
func TestSnowAndSeagrassItems(t *testing.T) {
	for _, n := range []string{"minecraft:snow_layer", "minecraft:seagrass"} {
		if _, ok := world.ItemByName(n, 0); !ok {
			t.Errorf("%s has no item", n)
		}
	}
	if d := (Seagrass{}).BreakInfo().Drops(item.Shears{}, nil); len(d) != 1 {
		t.Errorf("seagrass to shears drops %v", d)
	}
	if d := (Seagrass{}).BreakInfo().Drops(item.ToolNone{}, nil); len(d) != 0 {
		t.Errorf("seagrass by hand drops %v", d)
	}
	w := world.Config{}.New()
	defer w.Close()
	_, err := world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		tx.SetBlock(cube.Pos{0, 70, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{0, 71, 0}, block.Water{Still: true, Depth: 8}, nil)
		tx.SetBlock(cube.Pos{0, 72, 0}, block.Water{Still: true, Depth: 8}, nil)
		h := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 71, 2.5}}.New(player.Type, player.Config{Name: "t", Position: mgl64.Vec3{0.5, 71, 2.5}})
		p := tx.AddEntity(h).(*player.Player)
		p.SetGameMode(world.GameModeSurvival)
		p.SetHeldItems(item.NewStack(Seagrass{}, 2), item.Stack{})
		p.UseItemOnBlock(cube.Pos{0, 70, 0}, cube.FaceUp, mgl64.Vec3{0.5, 1, 0.5})
		if _, ok := tx.Block(cube.Pos{0, 71, 0}).(Seagrass); !ok {
			t.Errorf("seagrass item placed %T", tx.Block(cube.Pos{0, 71, 0}))
		}
		// Not on another seagrass: the water above it has no solid floor.
		p.UseItemOnBlock(cube.Pos{0, 71, 0}, cube.FaceUp, mgl64.Vec3{0.5, 1, 0.5})
		if _, ok := tx.Block(cube.Pos{0, 72, 0}).(Seagrass); ok {
			t.Error("seagrass placed on seagrass")
		}
		tx.SetBlock(cube.Pos{2, 70, 0}, block.Stone{}, nil)
		p.SetHeldItems(item.NewStack(SnowLayer{}, 1), item.Stack{})
		p.UseItemOnBlock(cube.Pos{2, 70, 0}, cube.FaceUp, mgl64.Vec3{0.5, 1, 0.5})
		if _, ok := tx.Block(cube.Pos{2, 71, 0}).(SnowLayer); !ok {
			t.Errorf("snow layer item placed %T", tx.Block(cube.Pos{2, 71, 0}))
		}
		return struct{}{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
