package javafront_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/bots/bedrock"
	"github.com/creeperdiamonds/overmind/internal/config"
	"github.com/creeperdiamonds/overmind/internal/testserver"
)

// commandTree is declare_commands as the bot's protocol.json parser read it.
type commandTree struct {
	Nodes []struct {
		Flags struct {
			Type       int `json:"command_node_type"`
			Executable int `json:"has_command"`
		} `json:"flags"`
		Children []int `json:"children"`
		Extra    *struct {
			Name   string `json:"name"`
			Parser string `json:"parser"`
		} `json:"extraNodeData"`
	} `json:"nodes"`
	Root int `json:"rootIndex"`
}

func (b *javaBot) commandTree() *commandTree {
	var r struct {
		Tree *commandTree `json:"tree"`
	}
	b.call("commandTree", nil, &r)
	return r.Tree
}

// systemTexts is every system chat line the bot got, as components.
func systemTexts(b *javaBot) []component {
	var out []component
	for _, l := range b.chat() {
		if l.Kind == "system" {
			out = append(out, plain(l.Msg))
		}
	}
	return out
}

func startGiveServer(t *testing.T, ops ...string) *testserver.Server {
	return testserver.StartWith(t, func(c *config.Config) { c.Operators = ops })
}

// An operator on Java is sent /give in its command tree, gives itself a sword
// with components in Java's syntax, and the slot it is shown holds exactly
// that; the reply is Java's own translation.
func TestJavaOperatorGivesItself(t *testing.T) {
	srv := startGiveServer(t, "jgiver") // any case
	j := startBot(t, srv, "JGiver")
	waitOnGround(t, srv, "JGiver")

	var tree *commandTree
	require.Eventually(t, func() bool { tree = j.commandTree(); return tree != nil }, 10*time.Second, 50*time.Millisecond)
	root := tree.Nodes[tree.Root]
	require.Len(t, root.Children, 1)
	path := []string{}
	for n := tree.Nodes[root.Children[0]]; ; n = tree.Nodes[n.Children[0]] {
		path = append(path, n.Extra.Name+":"+n.Extra.Parser)
		if len(n.Children) == 0 {
			break
		}
	}
	require.Equal(t, []string{"give:", "targets:minecraft:entity", "item:minecraft:item_stack", "count:brigadier:integer"}, path)

	j.command(`give @s minecraft:diamond_sword[enchantments={sharpness:5,unbreaking:3},custom_name='"Blade"',damage=10,unbreakable={},lore=["Sharp", "and old"]]`)
	require.Eventually(t, func() bool { return j.inventory().Slots[wHotbar] != nil }, 10*time.Second, 50*time.Millisecond, "nothing given: %+v", systemTexts(j))
	got := j.inventory().Slots[wHotbar]
	require.Equal(t, "diamond_sword", got.Name)
	require.Equal(t, 1, got.Count)
	data := func(typ string) string {
		d, ok := componentData(got, typ)
		require.True(t, ok, "no %s component: %+v", typ, got.Components)
		return string(d)
	}
	require.JSONEq(t, "10", data("damage"))
	require.Contains(t, data("custom_name"), "Blade")
	ench := data("enchantments")
	require.Contains(t, ench, `"level":5`)
	require.Contains(t, ench, `"level":3`)
	require.Contains(t, data("lore"), "and old", "lore with a space after a quote survives")
	_, ok := componentData(got, "unbreakable")
	require.True(t, ok)

	onJava(t, srv, "JGiver", func(p *player.Player) {
		s, _ := p.Inventory().Item(0)
		require.Equal(t, "Blade", s.CustomName())
		require.True(t, s.Unbreakable())
		require.Equal(t, []string{"Sharp", "and old"}, s.Lore())
		e, ok := s.Enchantment(enchantment.Sharpness)
		require.True(t, ok)
		require.Equal(t, 5, e.Level())
	})
	require.Eventually(t, func() bool {
		for _, c := range systemTexts(j) {
			if c.Translate == "commands.give.success.single" && len(c.With) == 3 && c.With[2].Text == "JGiver" &&
				len(c.With[1].With) == 1 && c.With[1].With[0].Text == "Blade" {
				return true
			}
		}
		return false
	}, 10*time.Second, 50*time.Millisecond, "%+v", systemTexts(j))

	// An unknown component is an error in red, and gives nothing.
	j.command("give @s stone[nosuch=1]")
	require.Eventually(t, func() bool {
		for _, c := range systemTexts(j) {
			if c.Color == "red" && strings.Contains(c.Text, "Unknown component 'minecraft:nosuch'") {
				return true
			}
		}
		return false
	}, 10*time.Second, 50*time.Millisecond, "%+v", systemTexts(j))
	require.Nil(t, j.inventory().Slots[wHotbar+1])
}

// A Java operator gives across editions: a Bedrock player by name, in any
// case, and everyone with @a.
func TestJavaOperatorGivesBedrock(t *testing.T) {
	srv := startGiveServer(t, "JCross")
	b, err := bedrock.Join(srv.BedrockAddr, "BTaker")
	require.NoError(t, err)
	defer b.Close()
	j := startBot(t, srv, "JCross")
	waitOnGround(t, srv, "JCross")

	j.command("give btaker stone 3")
	require.Eventually(t, func() bool { return slotCount(b.Inventory(), "minecraft:stone") == 3 }, 10*time.Second, 50*time.Millisecond, "%+v", systemTexts(j))
	j.command("give @a stick 2")
	require.Eventually(t, func() bool { return slotCount(b.Inventory(), "minecraft:stick") == 2 }, 10*time.Second, 50*time.Millisecond)
	require.Eventually(t, func() bool {
		s := j.inventory().Slots[wHotbar]
		return s != nil && s.Name == "stick" && s.Count == 2
	}, 10*time.Second, 50*time.Millisecond)
	require.Eventually(t, func() bool {
		return slices.ContainsFunc(systemTexts(j), func(c component) bool {
			return c.Translate == "commands.give.success.multiple" && len(c.With) == 3 && c.With[2].Text == "2"
		})
	}, 10*time.Second, 50*time.Millisecond, "%+v", systemTexts(j))

	// @p is the nearest player, here the giver itself, with a third player
	// between it and the Bedrock one (the fork's @p fix: upstream could pick
	// another of three).
	other := startBot(t, srv, "JOther")
	waitOnGround(t, srv, "JOther")
	at, _, _ := srv.JavaPosition("JCross")
	onJava(t, srv, "JOther", func(p *player.Player) { p.Teleport(at.Add(mgl64.Vec3{5, 0, 0})) })
	onJava(t, srv, "JCross", func(p *player.Player) { p.Teleport(at.Add(mgl64.Vec3{10, 0, 0})) })
	time.Sleep(500 * time.Millisecond)
	j.command("give @p apple 1")
	require.Eventually(t, func() bool {
		s := j.inventory().Slots[wHotbar+1]
		return s != nil && s.Name == "apple"
	}, 10*time.Second, 50*time.Millisecond, "%+v", systemTexts(j))
	require.Zero(t, slotCount(b.Inventory(), "minecraft:apple"))
	require.Nil(t, other.inventory().Slots[wHotbar])
}

func slotCount(slots []bedrock.Slot, name string) int {
	n := 0
	for _, s := range slots {
		if s.Name == name {
			n += s.Count
		}
	}
	return n
}

// A Bedrock operator sees /give in its command list and gives with both
// editions' syntax: Java's item[components], and Bedrock's item, amount, data.
func TestBedrockOperatorGivesBothSyntaxes(t *testing.T) {
	srv := startGiveServer(t, "BGiver")
	b, err := bedrock.Join(srv.BedrockAddr, "BGiver")
	require.NoError(t, err)
	defer b.Close()
	require.Eventually(t, func() bool { return slices.Contains(b.Commands(), "give") }, 15*time.Second, 50*time.Millisecond, "%v", b.Commands())

	outputs := func(want string) func() bool {
		return func() bool {
			return slices.ContainsFunc(b.CommandOutputs(), func(o bedrock.CommandOutput) bool { return o.Success && o.Message == want })
		}
	}
	require.NoError(t, b.Command(`/give @s diamond_sword[enchantments={sharpness:5},custom_name={text:"Edge",color:"gold"}]`))
	require.Eventually(t, outputs("Gave 1 [§6Edge] to BGiver"), 10*time.Second, 50*time.Millisecond, "%+v", b.CommandOutputs())
	require.NoError(t, b.Command("/give BGiver stone 5"))
	require.Eventually(t, outputs("Gave 5 [Stone] to BGiver"), 10*time.Second, 50*time.Millisecond, "%+v", b.CommandOutputs())
	require.NoError(t, b.Command("/give bgiver potion 2 5"))
	require.Eventually(t, outputs("Gave 2 [Potion] to BGiver"), 10*time.Second, 50*time.Millisecond, "%+v", b.CommandOutputs())

	require.Eventually(t, func() bool {
		inv := b.Inventory()
		return slotCount(inv, "minecraft:diamond_sword") == 1 && slotCount(inv, "minecraft:stone") == 5 && slotCount(inv, "minecraft:potion") == 2
	}, 10*time.Second, 50*time.Millisecond, "%+v", b.Inventory())
	onBedrock(t, srv, "BGiver", func(p *player.Player) {
		var sword, potion item.Stack
		for _, s := range p.Inventory().Items() {
			switch s.Item().(type) {
			case item.Sword:
				sword = s
			case item.Potion:
				potion = s
			}
		}
		require.Equal(t, "§6Edge", sword.CustomName())
		e, ok := sword.Enchantment(enchantment.Sharpness)
		require.True(t, ok)
		require.Equal(t, 5, e.Level())
		_, meta := potion.Item().EncodeItem()
		require.Equal(t, int16(5), meta)
	})

	// A bad item is an error, not a silent nothing.
	require.NoError(t, b.Command("/give @s nosuchitem"))
	require.Eventually(t, func() bool {
		return slices.ContainsFunc(b.CommandOutputs(), func(o bedrock.CommandOutput) bool {
			return !o.Success && strings.Contains(o.Message, "Unknown item 'minecraft:nosuchitem'")
		})
	}, 10*time.Second, 50*time.Millisecond, "%+v", b.CommandOutputs())
}

// A player not in Operators is not shown /give and is told, in its edition's
// words, that the command does not exist; nothing is given.
func TestNonOperatorRefused(t *testing.T) {
	srv := startGiveServer(t, "SomeoneElse")
	b, err := bedrock.Join(srv.BedrockAddr, "BPlain")
	require.NoError(t, err)
	defer b.Close()
	j := startBot(t, srv, "JPlain")
	waitOnGround(t, srv, "JPlain")

	// Java: the root alone, and vanilla's unknown command reply.
	var tree *commandTree
	require.Eventually(t, func() bool { tree = j.commandTree(); return tree != nil }, 10*time.Second, 50*time.Millisecond)
	require.Empty(t, tree.Nodes[tree.Root].Children)
	j.command("give @s diamond 5")
	require.Eventually(t, func() bool {
		texts := systemTexts(j)
		for i, c := range texts {
			if c.Translate == "command.unknown.command" && c.Color == "red" && i+1 < len(texts) {
				next := texts[i+1]
				return len(next.Extra) == 2 && next.Extra[0].Text == "give @s diamond 5" && next.Extra[1].Translate == "command.context.here"
			}
		}
		return false
	}, 10*time.Second, 50*time.Millisecond, "%+v", systemTexts(j))

	// Bedrock: not in the command list, and Dragonfly's unknown command.
	time.Sleep(time.Second) // the session has sent its commands by now
	require.NotContains(t, b.Commands(), "give")
	require.NoError(t, b.Command("/give @s diamond 5"))
	require.Eventually(t, func() bool {
		return slices.ContainsFunc(b.CommandOutputs(), func(o bedrock.CommandOutput) bool {
			return !o.Success && strings.Contains(o.Message, "commands.generic.unknown")
		})
	}, 10*time.Second, 50*time.Millisecond, "%+v", b.CommandOutputs())

	require.Zero(t, slotCount(b.Inventory(), "minecraft:diamond"))
	require.Nil(t, j.inventory().Slots[wHotbar])
	onJava(t, srv, "JPlain", func(p *player.Player) { require.Empty(t, p.Inventory().Items()) })
	onBedrock(t, srv, "BPlain", func(p *player.Player) { require.Empty(t, p.Inventory().Items()) })
}

// More than fits is split into stacks, and what does not fit is dropped at
// the player's feet as items.
func TestGiveOverflowDropsAtFeet(t *testing.T) {
	srv := startGiveServer(t, "JOverflow")
	j := startBot(t, srv, "JOverflow")
	waitOnGround(t, srv, "JOverflow")
	// One empty slot.
	onJava(t, srv, "JOverflow", func(p *player.Player) {
		for i := 1; i < 36; i++ {
			_ = p.Inventory().SetItem(i, item.NewStack(block.Dirt{}, 64))
		}
	})
	j.command("give @s stone 150")
	require.Eventually(t, func() bool {
		return slices.ContainsFunc(systemTexts(j), func(c component) bool { return c.Translate == "commands.give.success.single" })
	}, 10*time.Second, 50*time.Millisecond, "%+v", systemTexts(j))

	pos, _, ok := srv.JavaPosition("JOverflow")
	require.True(t, ok)
	onJava(t, srv, "JOverflow", func(p *player.Player) {
		s, _ := p.Inventory().Item(0)
		require.Equal(t, 64, s.Count())
		_, isStone := s.Item().(block.Stone)
		require.True(t, isStone)
		dropped := 0
		for e := range p.Tx().Entities() {
			if e.H().Type() != entity.ItemType {
				continue
			}
			it := e.(*entity.Ent).Behaviour().(*entity.ItemBehaviour).Item()
			require.Less(t, e.Position().Sub(pos).Len(), 1.5, "dropped away from the feet")
			if _, ok := it.Item().(block.Stone); ok {
				dropped += it.Count()
			}
		}
		require.Equal(t, 150-64, dropped)
	})

	// Above 100 stacks is refused, as vanilla does.
	j.command("give @s stone 6401")
	require.Eventually(t, func() bool {
		return slices.ContainsFunc(systemTexts(j), func(c component) bool {
			return c.Color == "red" && strings.Contains(c.Text, "Can't give more than 6400")
		})
	}, 10*time.Second, 50*time.Millisecond, "%+v", systemTexts(j))
}
