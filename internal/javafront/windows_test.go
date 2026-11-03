package javafront_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/stretchr/testify/require"

	"github.com/overmind/overmind/bots/bedrock"
	"github.com/overmind/overmind/internal/tables/java261"
	"github.com/overmind/overmind/internal/testserver"
)

// blockWindow is the block window the bot has open.
type blockWindow struct {
	Open *struct {
		ID    int32           `json:"id"`
		Type  int32           `json:"type"`
		Title json.RawMessage `json:"title"`
	} `json:"open"`
	Slots  []*botItem       `json:"slots"`
	Props  map[string]int16 `json:"props"`
	Cursor *botItem         `json:"cursor"`
	Opened int              `json:"opened"`
}

func (b *javaBot) window() blockWindow {
	var w blockWindow
	b.call("window", nil, &w)
	return w
}

// clickIn clicks a slot of a window (0 is the inventory).
func (b *javaBot) clickIn(window int32, slot, button, mode int) {
	b.call("click", map[string]any{"slot": slot, "button": button, "mode": mode, "window": window}, nil)
	time.Sleep(60 * time.Millisecond) // in order, and the answer back before the next
}

func (b *javaBot) closeIn(window int32) { b.call("closeWindow", map[string]any{"window": window}, nil) }

type botBlockEntity struct {
	Entity *struct {
		Type  int32          `json:"type"`
		NBT   map[string]any `json:"nbt"`
		Chunk bool           `json:"chunk"`
	} `json:"entity"`
}

func (b *javaBot) blockEntity(pos cube.Pos) botBlockEntity {
	var r botBlockEntity
	b.call("blockEntity", map[string]any{"x": pos[0], "y": pos[1], "z": pos[2]}, &r)
	return r
}

// name of a slot's item, "" when empty.
func nameOf(it *botItem) string {
	if it == nil {
		return ""
	}
	return it.Name
}

func countOf(it *botItem) int {
	if it == nil {
		return 0
	}
	return it.Count
}

// openBlock uses the block at pos with an empty hand and waits for a window of
// the menu type.
func openBlock(t *testing.T, srv *testserver.Server, j *javaBot, name string, pos cube.Pos, menu string, seq int) int32 {
	t.Helper()
	before := j.window().Opened
	onJava(t, srv, name, func(p *player.Player) { _ = p.SetHeldSlot(8) })
	time.Sleep(100 * time.Millisecond)
	j.place(pos, cube.FaceUp, 0.5, 1, 0.5, seq)
	var w blockWindow
	require.Eventually(t, func() bool { w = j.window(); return w.Opened > before && w.Open != nil }, 5*time.Second, 50*time.Millisecond, "no %s window", menu)
	require.Equal(t, java261.MenuType(menu), w.Open.Type)
	return w.Open.ID
}

// A Java player crafts by Java's recipes in its own 2x2 grid and at a
// crafting table, with plain clicks, a drag and a double click, and smelts in
// a furnace that a Bedrock player sees burning. Everything is checked on the
// server and in the client's windows.
func TestJavaCraftsAndSmelts(t *testing.T) {
	srv := testserver.Start(t)
	watcher, err := bedrock.Join(srv.BedrockAddr, "BKitchen")
	require.NoError(t, err)
	defer watcher.Close()
	j := startBot(t, srv, "JCrafter")
	waitOnGround(t, srv, "JCrafter")
	require.Eventually(t, func() bool { return j.column(2, 0, -61, -61).Loaded && j.column(-2, -2, -61, -61).Loaded }, 20*time.Second, 50*time.Millisecond)
	onJava(t, srv, "JCrafter", func(p *player.Player) {
		inv := p.Inventory()
		_ = inv.SetItem(0, item.NewStack(block.Log{Wood: block.OakWood()}, 2))
		_ = inv.SetItem(1, item.NewStack(block.Cobblestone{}, 8))
		_ = inv.SetItem(2, item.NewStack(item.RawGold{} /* 1 experience: always one orb */, 1))
		_ = inv.SetItem(3, item.NewStack(item.Coal{}, 1))
	})
	require.Eventually(t, func() bool { return nameOf(j.inventory().Slots[wHotbar+3]) == "coal" }, 5*time.Second, 50*time.Millisecond)
	have := func(it world.Item) int {
		n := 0
		onJava(t, srv, "JCrafter", func(p *player.Player) { n = count(p, it) })
		return n
	}
	oak := block.Planks{Wood: block.OakWood()}

	t.Run("planks from a log in the 2x2 grid", func(t *testing.T) {
		j.clickIn(0, wHotbar, 0, 0) // pick up both logs
		j.clickIn(0, 1, 1, 0)       // one into the grid
		j.clickIn(0, wHotbar, 0, 0) // the other back
		require.Eventually(t, func() bool { r := j.inventory().Slots[0]; return nameOf(r) == "oak_planks" && countOf(r) == 4 }, 5*time.Second, 50*time.Millisecond, "no result shown")
		j.clickIn(0, 0, 0, 1) // shift-click the result
		require.Eventually(t, func() bool { return have(oak) == 4 }, 5*time.Second, 50*time.Millisecond, "no planks crafted")
		require.Equal(t, 1, have(block.Log{Wood: block.OakWood()}), "the grid's log was not used up")
		require.Eventually(t, func() bool { inv := j.inventory(); return inv.Slots[0] == nil && inv.Slots[1] == nil }, 5*time.Second, 50*time.Millisecond)
	})
	t.Run("a full inventory takes no shift-crafted planks", func(t *testing.T) {
		var saved []item.Stack
		onJava(t, srv, "JCrafter", func(p *player.Player) {
			saved = p.Inventory().Slots()
			for i := 9; i < 36; i++ {
				_ = p.Inventory().SetItem(i, item.NewStack(block.Stone{}, 64))
			}
			for i := 4; i < 9; i++ {
				_ = p.Inventory().SetItem(i, item.NewStack(block.Stone{}, 64))
			}
			_ = p.Inventory().SetItem(1, item.NewStack(block.Stone{}, 64))
			_ = p.Inventory().SetItem(2, item.NewStack(block.Stone{}, 64))
			_ = p.Inventory().SetItem(3, item.NewStack(block.Stone{}, 64))
			_ = p.Inventory().SetItem(0, item.NewStack(block.Log{Wood: block.OakWood()}, 2))
		})
		require.Eventually(t, func() bool { return countOf(j.inventory().Slots[wHotbar]) == 2 }, 5*time.Second, 50*time.Millisecond)
		j.clickIn(0, wHotbar, 0, 0)
		j.clickIn(0, 1, 1, 0)
		j.clickIn(0, wHotbar, 0, 0) // every slot full again
		j.clickIn(0, 0, 0, 1)
		time.Sleep(300 * time.Millisecond)
		require.Equal(t, 0, have(oak), "planks were made with no room for them")
		require.Eventually(t, func() bool { return countOf(j.inventory().Slots[1]) == 1 }, 5*time.Second, 50*time.Millisecond, "the log left the grid")
		j.clickIn(0, 1, 0, 0) // take the log back out
		onJava(t, srv, "JCrafter", func(p *player.Player) {
			for i, s := range saved {
				_ = p.Inventory().SetItem(i, s)
			}
		})
		j.clickIn(0, 0, 0, 0) // the cursor's log: no result, nothing happens
		j.closeIn(0)          // the log goes back
		require.Eventually(t, func() bool { return have(block.Log{Wood: block.OakWood()}) == 2 }, 5*time.Second, 50*time.Millisecond)
		onJava(t, srv, "JCrafter", func(p *player.Player) {
			_ = p.Inventory().SetItem(0, item.NewStack(block.Log{Wood: block.OakWood()}, 1))
		})
		require.Eventually(t, func() bool { return countOf(j.inventory().Slots[wHotbar]) == 1 && j.inventory().Cursor == nil }, 5*time.Second, 50*time.Millisecond)
	})
	t.Run("sticks by dragging planks over two cells", func(t *testing.T) {
		var at int
		require.Eventually(t, func() bool {
			for i, s := range j.inventory().Slots {
				if i >= wMain && nameOf(s) == "oak_planks" {
					at = i
					return true
				}
			}
			return false
		}, 5*time.Second, 50*time.Millisecond)
		j.clickIn(0, at, 0, 0) // four planks on the cursor
		j.clickIn(0, -999, 0, 5)
		j.clickIn(0, 1, 1, 5)
		j.clickIn(0, 3, 1, 5)
		j.clickIn(0, -999, 2, 5) // two in each
		require.Eventually(t, func() bool {
			inv := j.inventory()
			return countOf(inv.Slots[1]) == 2 && countOf(inv.Slots[3]) == 2 && inv.Cursor == nil && nameOf(inv.Slots[0]) == "stick"
		}, 5*time.Second, 50*time.Millisecond, "the drag did not share the planks: %+v", j.inventory())
		j.clickIn(0, 0, 0, 0)
		j.clickIn(0, 0, 0, 0) // twice: eight sticks on the cursor
		require.Eventually(t, func() bool {
			inv := j.inventory()
			return nameOf(inv.Cursor) == "stick" && countOf(inv.Cursor) == 8 && inv.Slots[1] == nil && inv.Slots[3] == nil && inv.Slots[0] == nil
		}, 5*time.Second, 50*time.Millisecond, "each craft did not use a plank from each cell: %+v", j.inventory())
		j.clickIn(0, 20, 0, 0)
		require.Eventually(t, func() bool { return have(item.Stick{}) == 8 && have(oak) == 0 }, 5*time.Second, 50*time.Millisecond, "sticks %d, planks %d", have(item.Stick{}), have(oak))
	})
	t.Run("a double click gathers the sticks", func(t *testing.T) {
		onJava(t, srv, "JCrafter", func(p *player.Player) {
			_ = p.Inventory().SetItem(12, item.NewStack(item.Stick{}, 2))
			_ = p.Inventory().SetItem(13, item.NewStack(item.Stick{}, 3))
		})
		require.Eventually(t, func() bool { return countOf(j.inventory().Slots[13]) == 3 }, 5*time.Second, 50*time.Millisecond)
		j.clickIn(0, 20, 0, 0) // eight on the cursor
		j.clickIn(0, 20, 0, 6) // double click
		require.Eventually(t, func() bool {
			inv := j.inventory()
			return countOf(inv.Cursor) == 13 && inv.Slots[12] == nil && inv.Slots[13] == nil
		}, 5*time.Second, 50*time.Millisecond, "not gathered: %+v", j.inventory())
		j.clickIn(0, 20, 0, 0)
		require.Eventually(t, func() bool { return countOf(j.inventory().Slots[20]) == 13 }, 5*time.Second, 50*time.Millisecond)
		require.Equal(t, 13, have(item.Stick{}))
	})
	t.Run("a honey bottle leaves its bottle in the grid", func(t *testing.T) {
		onJava(t, srv, "JCrafter", func(p *player.Player) { _ = p.Inventory().SetItem(4, item.NewStack(item.HoneyBottle{}, 1)) })
		require.Eventually(t, func() bool { return nameOf(j.inventory().Slots[wHotbar+4]) == "honey_bottle" }, 5*time.Second, 50*time.Millisecond)
		j.clickIn(0, wHotbar+4, 0, 0)
		j.clickIn(0, 2, 0, 0)
		require.Eventually(t, func() bool { r := j.inventory().Slots[0]; return nameOf(r) == "sugar" && countOf(r) == 3 }, 5*time.Second, 50*time.Millisecond)
		j.clickIn(0, 0, 0, 1) // shift: into the inventory
		require.Eventually(t, func() bool { return have(item.Sugar{}) == 3 }, 5*time.Second, 50*time.Millisecond)
		require.Eventually(t, func() bool { return nameOf(j.inventory().Slots[2]) == "glass_bottle" }, 5*time.Second, 50*time.Millisecond, "the bottle did not stay in the grid")
		j.closeIn(0)
		require.Eventually(t, func() bool { return have(item.GlassBottle{}) == 1 }, 5*time.Second, 50*time.Millisecond)
	})
	t.Run("shift-crafting sugar with one free slot loses nothing", func(t *testing.T) {
		var saved []item.Stack
		onJava(t, srv, "JCrafter", func(p *player.Player) {
			saved = p.Inventory().Slots()
			for i := 1; i < 36; i++ {
				_ = p.Inventory().SetItem(i, item.NewStack(block.Stone{}, 64))
			}
			_ = p.Inventory().SetItem(0, item.NewStack(item.HoneyBottle{}, 2))
		})
		require.Eventually(t, func() bool { return countOf(j.inventory().Slots[wHotbar]) == 2 }, 5*time.Second, 50*time.Millisecond)
		j.clickIn(0, wHotbar, 0, 0) // both bottles: the hotbar's first slot is the one free slot
		j.clickIn(0, 2, 0, 0)
		require.Eventually(t, func() bool { return nameOf(j.inventory().Slots[0]) == "sugar" }, 5*time.Second, 50*time.Millisecond)
		j.clickIn(0, 0, 0, 1)
		time.Sleep(400 * time.Millisecond)
		// Each craft turns a honey bottle into 3 sugar and a glass bottle;
		// whatever happened, all of it is still somewhere.
		var honey, bottles, sugar int
		onJava(t, srv, "JCrafter", func(p *player.Player) {
			honey, bottles, sugar = count(p, item.HoneyBottle{}), count(p, item.GlassBottle{}), count(p, item.Sugar{})
		})
		inv := j.inventory()
		switch nameOf(inv.Slots[2]) {
		case "honey_bottle":
			honey += countOf(inv.Slots[2])
		case "glass_bottle":
			bottles += countOf(inv.Slots[2])
		}
		t.Logf("honey %d, bottles %d, sugar %d", honey, bottles, sugar)
		require.Equal(t, 2, honey+bottles, "a bottle was lost")
		require.Equal(t, 3*bottles, sugar, "sugar was lost")
		onJava(t, srv, "JCrafter", func(p *player.Player) {
			for i, s := range saved {
				_ = p.Inventory().SetItem(i, s)
			}
		})
		j.closeIn(0) // what is in the grid goes back, then the inventory is put back as it was
		require.Eventually(t, func() bool { return have(item.HoneyBottle{}) == 2 }, 5*time.Second, 50*time.Millisecond, "the grid's honey bottles did not come back")
		onJava(t, srv, "JCrafter", func(p *player.Player) {
			for i, s := range saved {
				_ = p.Inventory().SetItem(i, s)
			}
		})
		require.Eventually(t, func() bool { return have(item.HoneyBottle{}) == 0 && j.inventory().Cursor == nil }, 5*time.Second, 50*time.Millisecond)
	})
	t.Run("a furnace at a crafting table", func(t *testing.T) {
		table := cube.Pos{2, -60, 0}
		setBlock(t, srv, table, block.CraftingTable{})
		require.Eventually(t, func() bool { return j.block(table).Name == "crafting_table" }, 5*time.Second, 50*time.Millisecond)
		id := openBlock(t, srv, j, "JCrafter", table, "crafting", 30)
		cobble := 10 + 27 + 1 // the hotbar's second slot in a crafting window
		j.clickIn(id, cobble, 0, 0)
		for _, cell := range []int{1, 2, 3, 4, 6, 7, 8, 9} {
			j.clickIn(id, cell, 1, 0)
		}
		require.Eventually(t, func() bool { return nameOf(j.window().Slots[0]) == "furnace" }, 5*time.Second, 50*time.Millisecond, "no furnace shown: %+v", j.window())
		j.clickIn(id, 0, 0, 1)
		require.Eventually(t, func() bool { return have(block.NewFurnace(cube.North)) == 1 && have(block.Cobblestone{}) == 0 }, 5*time.Second, 50*time.Millisecond)
		// What is left in the grid comes back when the window closes.
		sticks := 10 + 11 // inventory slot 20 in a crafting window
		j.clickIn(id, sticks, 0, 0)
		j.clickIn(id, 5, 1, 0)
		j.clickIn(id, sticks, 0, 0)
		require.Eventually(t, func() bool { return countOf(j.window().Slots[5]) == 1 }, 5*time.Second, 50*time.Millisecond)
		require.Equal(t, 12, have(item.Stick{}))
		j.closeIn(id)
		require.Eventually(t, func() bool { return have(item.Stick{}) == 13 }, 5*time.Second, 50*time.Millisecond, "the grid's stick was kept")
	})
	t.Run("a furnace smelts, and Bedrock sees it burn", func(t *testing.T) {
		pos := cube.Pos{-2, -60, -2}
		setBlock(t, srv, pos, block.NewFurnace(cube.South))
		require.Eventually(t, func() bool { return j.block(pos).Name == "furnace" }, 5*time.Second, 50*time.Millisecond)
		id := openBlock(t, srv, j, "JCrafter", pos, "furnace", 31)
		// Dirt neither burns nor goes in the output, by click or by drag.
		onJava(t, srv, "JCrafter", func(p *player.Player) { _ = p.Inventory().SetItem(5, item.NewStack(block.Dirt{}, 4)) })
		require.Eventually(t, func() bool { return countOf(j.inventory().Slots[wHotbar+5]) == 4 }, 5*time.Second, 50*time.Millisecond)
		j.clickIn(id, 3+27+5, 0, 0)
		j.clickIn(id, 2, 0, 0)
		j.clickIn(id, 1, 1, 0)
		j.clickIn(id, -999, 4, 5)
		j.clickIn(id, 1, 5, 5)
		j.clickIn(id, 2, 5, 5)
		j.clickIn(id, -999, 6, 5)
		j.clickIn(id, 3+27+5, 0, 0)
		require.Eventually(t, func() bool {
			w := j.window()
			return w.Slots[1] == nil && w.Slots[2] == nil && countOf(w.Slots[3+27+5]) == 4 && w.Cursor == nil
		}, 5*time.Second, 50*time.Millisecond, "dirt went into the fuel or output slot: %+v", j.window())
		require.Equal(t, 4, have(block.Dirt{}))
		// Nor do water and milk buckets, which do not burn, or empty buckets
		// but one at a time.
		for _, st := range []item.Stack{
			item.NewStack(item.Bucket{Content: item.LiquidBucketContent(block.Water{Still: true, Depth: 8})}, 1),
			item.NewStack(item.Bucket{Content: item.MilkBucketContent()}, 1),
			item.NewStack(item.Bucket{}, 2),
		} {
			onJava(t, srv, "JCrafter", func(p *player.Player) { _ = p.Inventory().SetItem(6, st) })
			require.Eventually(t, func() bool { return countOf(j.inventory().Slots[wHotbar+6]) == st.Count() }, 5*time.Second, 50*time.Millisecond, "%v not shown", st)
			j.clickIn(id, 3+27+6, 0, 0)
			j.clickIn(id, 1, 0, 0)
			j.clickIn(id, 3+27+6, 0, 0)
			j.clickIn(id, 3+27+6, 0, 1) // and shift-clicked
			require.Eventually(t, func() bool {
				w := j.window()
				return w.Slots[1] == nil && w.Cursor == nil
			}, 5*time.Second, 50*time.Millisecond, "%v went into the fuel slot: %+v", st, j.window())
			require.Never(t, func() bool { return j.window().Slots[1] != nil }, 300*time.Millisecond, 50*time.Millisecond, "%v went into the fuel slot", st)
			onJava(t, srv, "JCrafter", func(p *player.Player) {
				for i := 0; i < 36; i++ {
					if s, _ := p.Inventory().Item(i); s.Comparable(st) {
						_ = p.Inventory().SetItem(i, item.Stack{})
					}
				}
			})
		}
		j.clickIn(id, 3+27+2, 0, 1) // raw gold, shift-clicked, goes in to smelt
		j.clickIn(id, 3+27+3, 0, 1) // coal, as fuel
		require.Eventually(t, func() bool {
			w := j.window()
			return nameOf(w.Slots[0]) == "raw_gold" && nameOf(w.Slots[1]) == "" && w.Props["0"] > 0 && w.Props["3"] == 200
		}, 5*time.Second, 50*time.Millisecond, "not burning: %+v", j.window())
		lit := world.BlockRuntimeID(block.Furnace{Facing: cube.South, Lit: true})
		require.Eventually(t, func() bool { got, ok := watcher.Block(pos); return ok && got == lit }, 5*time.Second, 50*time.Millisecond, "Bedrock does not see it lit")
		shows(t, j, pos, "furnace", map[string]any{"lit": true})
		require.Eventually(t, func() bool { return j.window().Props["2"] > 20 }, 5*time.Second, 50*time.Millisecond, "no progress shown")
		require.Eventually(t, func() bool { return nameOf(j.window().Slots[2]) == "gold_ingot" }, 15*time.Second, 100*time.Millisecond, "nothing smelted")
		j.clickIn(id, 2, 0, 1)
		require.Eventually(t, func() bool { return have(item.GoldIngot{}) == 1 }, 5*time.Second, 50*time.Millisecond)
		// Taking the output releases the furnace's experience, as for Bedrock.
		require.Eventually(t, func() bool {
			xp := 0
			onJava(t, srv, "JCrafter", func(p *player.Player) {
				xp = p.Experience()
				for e := range p.Tx().Entities() {
					if e.H().Type() == entity.ExperienceOrbType {
						xp++
					}
				}
			})
			return xp > 0
		}, 5*time.Second, 50*time.Millisecond, "no experience for the smelted gold")
		j.closeIn(id)
	})
	t.Run("a chest takes sticks", func(t *testing.T) {
		pos := cube.Pos{1, -60, -2}
		setBlock(t, srv, pos, block.NewChest())
		require.Eventually(t, func() bool { return j.block(pos).Name == "chest" }, 5*time.Second, 50*time.Millisecond)
		id := openBlock(t, srv, j, "JCrafter", pos, "generic_9x3", 32)
		j.clickIn(id, 27+20-9, 0, 1) // inventory slot 20 is window slot 27 + 11
		require.Eventually(t, func() bool { return nameOf(j.window().Slots[0]) == "stick" }, 5*time.Second, 50*time.Millisecond, "%+v", j.window())
		var inChest item.Stack
		_, err := world.Call(t.Context(), srv.World(), func(tx *world.Tx) (struct{}, error) {
			inChest, _ = tx.Block(pos).(block.Chest).Inventory(tx, pos).Item(0)
			return struct{}{}, nil
		})
		require.NoError(t, err)
		require.Equal(t, 13, inChest.Count())
		j.closeIn(id)
		require.Equal(t, 0, have(item.Stick{}))
	})
}

// A sign's text goes both ways: a Java player writes it in the editor the
// server opened, a Bedrock player's edit reaches Java, and a Java player who
// comes later reads it from the chunk.
func TestSignsBothWays(t *testing.T) {
	srv := testserver.Start(t)
	b, err := bedrock.Join(srv.BedrockAddr, "BScribe")
	require.NoError(t, err)
	defer b.Close()
	j := startBot(t, srv, "JScribe")
	reader := startBot(t, srv, "JReader")
	waitOnGround(t, srv, "JScribe")
	require.Eventually(t, func() bool { return j.column(2, 0, -61, -61).Loaded }, 20*time.Second, 50*time.Millisecond)

	holdJava(t, srv, "JScribe", item.NewStack(block.Sign{Wood: block.OakWood()}, 1))
	j.look(0, 45)
	time.Sleep(150 * time.Millisecond)
	j.place(cube.Pos{2, -61, 0}, cube.FaceUp, 0.5, 1, 0.5, 5)
	pos := cube.Pos{2, -60, 0}
	var opened struct {
		Opens []struct {
			X, Y, Z int
			Front   bool
		} `json:"opens"`
	}
	require.Eventually(t, func() bool { j.call("signOpens", nil, &opened); return len(opened.Opens) > 0 }, 5*time.Second, 50*time.Millisecond, "no sign editor")
	require.Equal(t, cube.Pos{opened.Opens[0].X, opened.Opens[0].Y, opened.Opens[0].Z}, pos)
	require.True(t, opened.Opens[0].Front)

	signText := func() (front, back string) {
		_, err := world.Call(t.Context(), srv.World(), func(tx *world.Tx) (struct{}, error) {
			s := tx.Block(pos).(block.Sign)
			front, back = s.Front.Text, s.Back.Text
			return struct{}{}, nil
		})
		require.NoError(t, err)
		return
	}
	lines := func(bot *javaBot) []any {
		e := bot.blockEntity(pos).Entity
		if e == nil {
			return nil
		}
		f, _ := e.NBT["front_text"].(map[string]any)
		m, _ := f["messages"].([]any)
		return m
	}
	j.call("updateSign", map[string]any{"x": pos[0], "y": pos[1], "z": pos[2], "front": true, "lines": []string{"Hello", "from §Java", "", ""}}, nil)
	require.Eventually(t, func() bool { f, _ := signText(); return f == "Hello\nfrom Java" }, 5*time.Second, 50*time.Millisecond, "the sign was not written")
	require.Eventually(t, func() bool { m := lines(reader); return len(m) == 4 && m[0] == "Hello" && m[1] == "from Java" }, 5*time.Second, 50*time.Millisecond, "another Java player did not see it: %v", lines(reader))
	require.Equal(t, java261.BlockEntityType("sign"), reader.blockEntity(pos).Entity.Type)

	// A second update without the editor open is refused.
	j.call("updateSign", map[string]any{"x": pos[0], "y": pos[1], "z": pos[2], "front": true, "lines": []string{"sneaky"}}, nil)
	time.Sleep(300 * time.Millisecond)
	f, _ := signText()
	require.Equal(t, "Hello\nfrom Java", f)

	// An update for the side not opened is refused.
	onJava(t, srv, "JScribe", func(p *player.Player) { p.OpenSign(pos, true) })
	time.Sleep(100 * time.Millisecond)
	j.call("updateSign", map[string]any{"x": pos[0], "y": pos[1], "z": pos[2], "front": false, "lines": []string{"wrong side"}}, nil)
	time.Sleep(300 * time.Millisecond)
	f, back := signText()
	require.Equal(t, "Hello\nfrom Java", f)
	require.Equal(t, "", back, "the back was written though the front was opened")
	// An open for a sign broken and replaced since is refused.
	onJava(t, srv, "JScribe", func(p *player.Player) { p.OpenSign(pos, true) })
	setBlock(t, srv, pos, block.Air{})
	setBlock(t, srv, pos, block.Sign{Wood: block.OakWood()})
	j.call("updateSign", map[string]any{"x": pos[0], "y": pos[1], "z": pos[2], "front": true, "lines": []string{"stale open"}}, nil)
	time.Sleep(300 * time.Millisecond)
	f, _ = signText()
	require.Equal(t, "", f, "an open from before the sign was replaced wrote it")
	// Out of Java's reach is refused.
	far := cube.Pos{12, -60, 0}
	setBlock(t, srv, far, block.Sign{Wood: block.OakWood()})
	onJava(t, srv, "JScribe", func(p *player.Player) { p.OpenSign(far, true) })
	time.Sleep(100 * time.Millisecond)
	j.call("updateSign", map[string]any{"x": far[0], "y": far[1], "z": far[2], "front": true, "lines": []string{"far"}}, nil)
	time.Sleep(300 * time.Millisecond)
	farText, err := world.Call(t.Context(), srv.World(), func(tx *world.Tx) (string, error) {
		return tx.Block(far).(block.Sign).Front.Text, nil
	})
	require.NoError(t, err)
	require.Equal(t, "", farText, "a sign out of reach was written")

	onBedrock(t, srv, "BScribe", func(p *player.Player) { require.NoError(t, p.EditSign(pos, "From\nBedrock", "")) })
	require.Eventually(t, func() bool { m := lines(j); return len(m) == 4 && m[0] == "From" && m[1] == "Bedrock" && m[2] == "" }, 5*time.Second, 50*time.Millisecond, "Java did not see Bedrock's edit: %v", lines(j))

	late := startBot(t, srv, "JLateReader")
	require.Eventually(t, func() bool { return late.column(2, 0, -61, -61).Loaded }, 20*time.Second, 50*time.Millisecond)
	require.Eventually(t, func() bool { m := lines(late); return len(m) == 4 && m[0] == "From" }, 5*time.Second, 50*time.Millisecond)
	require.True(t, late.blockEntity(pos).Entity.Chunk, "not from the chunk")
}
