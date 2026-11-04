package block_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

// Overmind fork: leaf decay by edition. The Bedrock cases repeat what a Bedrock Dedicated Server 1.26.60 did
// with leaves set by console commands (Task W report): a leaf stays while a log is within 4 steps through
// leaves, face to face; only the removal of a log (a 9 x 9 x 9 cube around it) or a leaf (3 x 3 x 3)
// marks leaves for the check, and placing or removing any other block beside a leaf marks nothing. Java
// keeps leaves up to 6 steps from a log (minecraft.wiki "Leaves", oldid 3873085) and any change beside a
// leaf has it checked.

const leafY = 100

// leafWorld is a synchronous world of the given leaf rules whose every block gets random ticks.
func leafWorld(t *testing.T, java bool) *world.World {
	t.Helper()
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry, RandomTickSpeed: 4096,
		Mechanics: world.Mechanics{JavaLeafDecay: java}}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func markedLeaf(marked bool) block.Leaves { return block.Leaves{Type: block.OakLeaves(), ShouldUpdate: marked} }

func setLeafBlocks(w *world.World, blocks map[cube.Pos]world.Block) {
	w.Do(func(tx *world.Tx) {
		for p, b := range blocks {
			tx.SetBlock(p, b, nil)
		}
	})
}

func isLeafAt(w *world.World, p cube.Pos) (ok bool) {
	w.Do(func(tx *world.Tx) { _, ok = tx.Block(p).(block.Leaves) })
	return ok
}

func leafTicks(w *world.World, n int) {
	for range n {
		w.AdvanceTick()
	}
}

// line puts a log at x 0 and leaves at x 1 to n along z, and returns the leaves' positions.
func leafLine(w *world.World, z, n int, marked bool) []cube.Pos {
	b := map[cube.Pos]world.Block{{0, leafY, z}: block.Log{Wood: block.OakWood()}}
	var leaves []cube.Pos
	for k := 1; k <= n; k++ {
		p := cube.Pos{k, leafY, z}
		b[p] = markedLeaf(marked)
		leaves = append(leaves, p)
	}
	setLeafBlocks(w, b)
	return leaves
}

// survivors is how many of the leaves are still there, counted from the log.
func leafSurvivors(w *world.World, leaves []cube.Pos) (n int) {
	for _, p := range leaves {
		if !isLeafAt(w, p) {
			return n
		}
		n++
	}
	return n
}

func TestLeafReachByEdition(t *testing.T) {
	for _, c := range []struct {
		java bool
		want int
	}{{false, 4}, {true, 6}} {
		w := leafWorld(t, c.java)
		leaves := leafLine(w, 0, 8, true)
		leafTicks(w, 40)
		if got := leafSurvivors(w, leaves); got != c.want {
			t.Errorf("java %v: %d leaves of a marked line stayed, want %d", c.java, got, c.want)
		}
	}
}

// Bedrock counts the steps face to face through leaves: an L of 3 along x and 3 along z keeps 4.
func TestBedrockLeafReachTurnsCorners(t *testing.T) {
	w := leafWorld(t, false)
	path := []cube.Pos{{1, leafY, 0}, {2, leafY, 0}, {3, leafY, 0}, {3, leafY, 1}, {3, leafY, 2}, {3, leafY, 3}}
	b := map[cube.Pos]world.Block{{0, leafY, 0}: block.Log{Wood: block.OakWood()}}
	for _, p := range path {
		b[p] = markedLeaf(true)
	}
	setLeafBlocks(w, b)
	leafTicks(w, 40)
	if got := leafSurvivors(w, path); got != 4 {
		t.Errorf("%d leaves of a marked L stayed, want 4", got)
	}
}

// On Bedrock a block placed or removed beside a leaf too far from its log leaves it alone.
func TestBedrockOtherBlocksMarkNothing(t *testing.T) {
	w := leafWorld(t, false)
	placed := leafLine(w, 0, 7, false)
	removed := leafLine(w, 10, 7, false)
	setLeafBlocks(w, map[cube.Pos]world.Block{{7, leafY + 1, 0}: block.Stone{}, {7, leafY + 1, 10}: block.Stone{}})
	leafTicks(w, 5)
	setLeafBlocks(w, map[cube.Pos]world.Block{{7, leafY + 1, 10}: block.Air{}})
	leafTicks(w, 40)
	if got := leafSurvivors(w, placed); got != 7 {
		t.Errorf("a stone placed beside the line: %d of 7 leaves stayed", got)
	}
	if got := leafSurvivors(w, removed); got != 7 {
		t.Errorf("a stone removed beside the line: %d of 7 leaves stayed", got)
	}
}

// On Java any change beside a leaf has it checked, and the check spreads through the leaves.
func TestJavaChangeBesideLeafMarksIt(t *testing.T) {
	w := leafWorld(t, true)
	leaves := leafLine(w, 0, 8, false)
	setLeafBlocks(w, map[cube.Pos]world.Block{{1, leafY + 1, 0}: block.Stone{}})
	leafTicks(w, 60)
	if got := leafSurvivors(w, leaves); got != 6 {
		t.Errorf("a stone placed beside the line: %d of 8 leaves stayed, want 6", got)
	}
}

// On Bedrock removing a log marks the leaves in a 9 x 9 x 9 cube around it, and removing a leaf the
// leaves in a 3 x 3 x 3 cube; a line whose fifth leaf is too far from its log loses it only when it is
// marked.
func TestBedrockRemovalMarks(t *testing.T) {
	for _, c := range []struct {
		name   string
		b      world.Block
		offset cube.Pos
		decays bool
	}{
		{"log 4 away", block.Log{Wood: block.OakWood()}, cube.Pos{4, 0, 0}, true},
		{"log 5 away", block.Log{Wood: block.OakWood()}, cube.Pos{5, 0, 0}, false},
		{"log 4, 4, 4 away", block.Log{Wood: block.OakWood()}, cube.Pos{4, 4, 4}, true},
		{"log 5, 4, 0 away", block.Log{Wood: block.OakWood()}, cube.Pos{5, 4, 0}, false},
		{"leaf beside", block.Leaves{Type: block.OakLeaves(), Persistent: true}, cube.Pos{1, 0, 0}, true},
		{"leaf 1, 1, 1 away", block.Leaves{Type: block.OakLeaves(), Persistent: true}, cube.Pos{1, 1, 1}, true},
		{"leaf 2 away", block.Leaves{Type: block.OakLeaves(), Persistent: true}, cube.Pos{2, 0, 0}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := leafWorld(t, false)
			leaves := leafLine(w, 0, 5, false)
			at := cube.Pos{5, leafY, 0}.Add(c.offset)
			setLeafBlocks(w, map[cube.Pos]world.Block{at: c.b})
			leafTicks(w, 5)
			setLeafBlocks(w, map[cube.Pos]world.Block{at: block.Air{}})
			leafTicks(w, 40)
			want := 5
			if c.decays {
				want = 4
			}
			if got := leafSurvivors(w, leaves); got != want {
				t.Errorf("%d of 5 leaves stayed, want %d", got, want)
			}
		})
	}
}

// blockUpdates counts the block updates a viewer is sent, by position.
type blockUpdates struct {
	world.NopViewer
	at map[cube.Pos]int
}

func (v *blockUpdates) ViewBlockUpdate(pos cube.Pos, _ world.Block, _ int) { v.at[pos]++ }

// A leaf's decay mark is not part of what a viewer is shown: marking leaves when a log is cut, and
// clearing the mark when a leaf finds another log, send viewers nothing. Only the log's removal is sent.
func TestLeafMarksSendNothing(t *testing.T) {
	for _, java := range []bool{false, true} {
		w := leafWorld(t, java)
		v := &blockUpdates{at: map[cube.Pos]int{}}
		l := world.NewLoader(1, w, v)
		w.Do(func(tx *world.Tx) { l.Load(tx, 9) })
		cut, kept := cube.Pos{8, leafY, 8}, cube.Pos{9, leafY, 8}
		b := map[cube.Pos]world.Block{cut: block.Log{Wood: block.OakWood()}, kept: block.Log{Wood: block.OakWood()}}
		var leaves []cube.Pos
		for x := 7; x <= 10; x++ {
			for y := leafY - 1; y <= leafY+1; y++ {
				for z := 7; z <= 9; z++ {
					if p := (cube.Pos{x, y, z}); p != cut && p != kept {
						b[p] = markedLeaf(false)
						leaves = append(leaves, p)
					}
				}
			}
		}
		setLeafBlocks(w, b)
		clear(v.at)
		setLeafBlocks(w, map[cube.Pos]world.Block{cut: block.Air{}})
		leafTicks(w, 40)
		marked := 0
		for _, p := range leaves {
			if !isLeafAt(w, p) {
				t.Fatalf("java %v: the leaf at %v decayed beside a log", java, p)
			}
			marked += v.at[p]
		}
		if marked != 0 {
			t.Errorf("java %v: viewers were sent %d block updates of leaves whose look did not change", java, marked)
		}
		if v.at[cut] == 0 {
			t.Errorf("java %v: the log's removal was not sent", java)
		}
		w.Do(func(tx *world.Tx) { l.Close(tx) })
	}
}
