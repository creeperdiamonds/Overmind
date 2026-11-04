package java261

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/stretchr/testify/require"
)

// itemNames reads the display names of the Bedrock and Java item lists
// (minecraft-data's bedrock items.json and pc 26.1 items.json).
func itemNames(t *testing.T) (bedrock, java map[string]string) {
	raw, err := os.ReadFile("testdata/itemnames.json")
	require.NoError(t, err)
	var m map[string]map[string]string
	require.NoError(t, json.Unmarshal(raw, &m))
	return m["bedrock"], m["java"]
}

// javaBlockOf is the Java block name of a Java state, from the reference copy
// of the Java block list.
func javaBlockOf(t *testing.T, java map[string]*javaBlock, state uint16) string {
	for name, b := range java {
		if int(state) >= b.MinStateID && int(state) <= b.MaxStateID {
			return strings.TrimPrefix(name, "minecraft:")
		}
	}
	t.Fatalf("no Java block has state %d", state)
	return ""
}

// A Dragonfly block item maps to the Java item of the Java block its block
// maps to, where Java has an item of that name. A Bedrock name that is also a
// different Java item's name must not win: Bedrock's stone_stairs is the
// cobblestone stairs, its snow the snow block, its nether_brick the nether
// bricks block.
func TestBlockItemsAreTheirBlocks(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	blocks := loadJava(t)
	items := javaItems(t)
	itemName := map[int32]string{}
	for n, id := range items {
		itemName[id] = n
	}
	// Block items whose block is not the item on Java: a bed's colour is its
	// meta (white is 0) and its block entity, not its block state; wheat seeds
	// place the wheat crop, and Java's wheat item is the harvest; item frames
	// are blocks on Bedrock but entities on Java, so their block shows as stone.
	skip := map[string]bool{"minecraft:bed": true, "minecraft:wheat_seeds": true, "minecraft:frame": true, "minecraft:glow_frame": true}
	checked := 0
	var wrong []string
	for _, it := range world.Items() {
		b, isBlock := it.(world.Block)
		name, meta := it.EncodeItem()
		if !isBlock || meta != 0 || skip[name] {
			continue
		}
		id, ok := JavaItem(name, 0)
		if !ok {
			continue
		}
		jb := javaBlockOf(t, blocks, JavaState(world.BlockRuntimeID(b)))
		want, has := items[jb]
		if !has {
			continue // Java has no item named like the block (wall signs, crops, wire)
		}
		checked++
		if id != want {
			wrong = append(wrong, name+" gives "+itemName[id]+", its block is "+jb)
		}
	}
	sort.Strings(wrong)
	t.Logf("%d block items checked", checked)
	require.Greater(t, checked, 800)
	require.Empty(t, wrong)
}

// notCounterparts are Bedrock items whose display name is a Java item's but
// which are something else, or a form Java has no item for.
var notCounterparts = map[string]string{
	"deprecated_anvil":  "a legacy block, not an item a player holds",
	"element_82":        "Education Edition's lead, the element",
	"underwater_tnt":    "Education Edition's underwater TNT",
	"light_block":       "the light block of no level; Java's light is light_block_0 to 15",
	"crimson_wall_sign": "the wall form of the sign block; the item is crimson_sign",
	"warped_wall_sign":  "the wall form of the sign block; the item is warped_sign",
	"redstone_wire":     "the wire block; the item is redstone",
	// minecraft-data's Bedrock display name is Stone Slab, but its block is the
	// double smooth stone slab (blocksB2J), so Java's smooth_stone_slab.
	"smooth_stone_double_slab": "the double smooth stone slab, shown on Bedrock as Stone Slab",
}

// Every Bedrock item that is one Java item by its display name maps to that
// item, and it comes back through BedrockItem to a Bedrock name that maps to
// it again.
func TestEveryBedrockItemWithJavaCounterpartRoundTrips(t *testing.T) {
	bedrock, java := itemNames(t)
	ids := javaItems(t)
	byDisplay := map[string][]string{}
	for n, d := range java {
		byDisplay[strings.ToLower(d)] = append(byDisplay[strings.ToLower(d)], n)
	}
	checked := 0
	var bad []string
	for b, d := range bedrock {
		if b == "unknown" || strings.HasPrefix(b, "item.") || notCounterparts[b] != "" {
			continue
		}
		cands := byDisplay[strings.ToLower(d)]
		if len(cands) != 1 {
			continue
		}
		want := cands[0]
		checked++
		id, ok := JavaItem(b, 0)
		switch {
		case !ok:
			bad = append(bad, b+" ("+d+"): no Java item, want "+want)
			continue
		case id != ids[want]:
			bad = append(bad, b+" ("+d+"): wrong Java item, want "+want)
			continue
		}
		back, meta, ok := BedrockItem(id)
		if !ok {
			bad = append(bad, want+": no Bedrock item back")
			continue
		}
		if again, ok := JavaItem(back, meta); !ok || again != id {
			bad = append(bad, want+": back to "+back+", which does not map to it")
		}
	}
	sort.Strings(bad)
	t.Logf("%d Bedrock items with one Java counterpart checked", checked)
	require.Greater(t, checked, 1200)
	require.Empty(t, bad)
}

// Every Java item a Bedrock item maps to has a Bedrock item to go back to,
// which maps to it again. The tipped arrow is the one without: its Bedrock
// form is an arrow with a potion as meta, which the Java item does not say.
func TestEveryMappedJavaItemGoesBack(t *testing.T) {
	bedrock, _ := itemNames(t)
	ids := javaItems(t)
	var missing []string
	for b := range bedrock {
		id, ok := JavaItem(b, 0)
		if !ok || id == ids["tipped_arrow"] {
			continue
		}
		back, meta, ok := BedrockItem(id)
		if !ok {
			missing = append(missing, b)
			continue
		}
		again, ok := JavaItem(back, meta)
		require.True(t, ok, back)
		require.Equal(t, id, again, "%s goes back to %s", b, back)
	}
	sort.Strings(missing)
	require.Empty(t, missing, "Java items of these Bedrock items have no Bedrock item to go back to")
}
