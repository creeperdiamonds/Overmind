package give

import (
	"image/color"
	"strings"
	"testing"

	_ "github.com/df-mc/dragonfly/server/block" // registers block items
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSNBT(t *testing.T) {
	v, err := ParseSNBT(`{a:1b, 'b c':"x\"y", list:[1, 2.5f, -3L], arr:[I; 1, 2], t:true, s:word, n:{}, h:0x7FFF33, u:1_000}`)
	require.NoError(t, err)
	c := v.(Compound)
	require.Equal(t, []string{"a", "b c", "list", "arr", "t", "s", "n", "h", "u"}, c.Keys)
	require.Equal(t, int64(1), c.Values["a"])
	require.Equal(t, `x"y`, c.Values["b c"])
	require.Equal(t, []any{int64(1), 2.5, int64(-3)}, c.Values["list"])
	require.Equal(t, []any{int64(1), int64(2)}, c.Values["arr"])
	require.Equal(t, true, c.Values["t"])
	require.Equal(t, "word", c.Values["s"])
	require.Equal(t, Compound{Values: map[string]any{}}, c.Values["n"])
	require.Equal(t, int64(0x7FFF33), c.Values["h"])
	require.Equal(t, int64(1000), c.Values["u"])

	v, err = ParseSNBT(`'it\'s é'`)
	require.NoError(t, err)
	require.Equal(t, "it's é", v)

	for _, bad := range []string{`{a:1`, `{a 1}`, `[1,`, `"open`, `{a:1,a:2}`, `1.2.3`, `{a:1} x`, `'\q'`} {
		_, err := ParseSNBT(bad)
		require.Error(t, err, bad)
	}
}

func TestParseRequest(t *testing.T) {
	r, err := ParseRequest(`minecraft:diamond_sword[enchantments={sharpness:5,unbreaking:3},custom_name='"Blade"',damage=10,unbreakable={}]`)
	require.NoError(t, err)
	require.Equal(t, "diamond_sword", r.Item)
	require.Equal(t, 1, r.Count)
	require.Equal(t, -1, r.Data)
	var names []string
	for _, c := range r.Components {
		names = append(names, c.Name)
	}
	require.Equal(t, []string{"enchantments", "custom_name", "damage", "unbreakable"}, names)

	r, err = ParseRequest("stone 64")
	require.NoError(t, err)
	require.Equal(t, Request{Item: "stone", Count: 64, Data: -1}, r)

	// Bedrock's order: item, amount, data.
	r, err = ParseRequest("wool 3 14")
	require.NoError(t, err)
	require.Equal(t, Request{Item: "wool", Count: 3, Data: 14}, r)

	// Spaces inside a component value.
	r, err = ParseRequest(`stick[lore=["one", "two"], custom_name={text: "My Wand", color: "gold"}] 2`)
	require.NoError(t, err)
	require.Equal(t, 2, r.Count)
	require.Equal(t, []any{"one", "two"}, r.Components[0].Value)

	for bad, want := range map[string]string{
		"":                             "Expected an item",
		"stone 0":                      "less than 1",
		"stone x":                      "Invalid integer",
		"stone 1 2 3":                  "Incorrect argument",
		"stone[damage=1":               "Expected",
		"stone[damage=1,damage=2]":     "repeated",
		"stone[!damage]":               "not supported",
		"other:stone":                  "Unknown item",
		`stone 1 0 {"can_destroy":{}}`: "JSON",
		"stone[damage]":                "Expected '='",
		"stone[]x":                     "trailing data",
	} {
		_, err := ParseRequest(bad)
		require.ErrorContains(t, err, want, bad)
	}
}

func build(t *testing.T, text string) Built {
	t.Helper()
	r, err := ParseRequest(text)
	require.NoError(t, err, text)
	b, err := Build(r)
	require.NoError(t, err, text)
	return b
}

func TestBuild(t *testing.T) {
	b := build(t, `minecraft:diamond_sword[enchantments={sharpness:5,unbreaking:3},custom_name='"Blade"',damage=10,unbreakable={}]`)
	s := b.Stack
	_, isSword := s.Item().(item.Sword)
	require.True(t, isSword)
	require.Equal(t, "Blade", s.CustomName())
	require.Equal(t, "Blade", b.Name)
	require.Equal(t, s.MaxDurability()-10, s.Durability())
	require.True(t, s.Unbreakable())
	e, ok := s.Enchantment(enchantment.Sharpness)
	require.True(t, ok)
	require.Equal(t, 5, e.Level())
	e, ok = s.Enchantment(enchantment.Unbreaking)
	require.True(t, ok)
	require.Equal(t, 3, e.Level())
	require.Equal(t, "diamond_sword", b.JavaName)
	require.False(t, b.Block)

	// Text: plain, SNBT compound with colour and style, JSON, and lore.
	require.Equal(t, "Wand", build(t, `stick[custom_name="Wand"]`).Stack.CustomName())
	require.Equal(t, "§6§lMy §r§6§l§oWand", build(t, `stick[custom_name={text:"My ",color:gold,bold:true,extra:[{text:"Wand",italic:1b}]}]`).Stack.CustomName())
	require.Equal(t, "§cHot", build(t, `stick[custom_name='{"text":"Hot","color":"red"}']`).Stack.CustomName())
	require.Equal(t, "§cNear", build(t, `stick[custom_name={text:"Near",color:"#FF5050"}]`).Stack.CustomName())
	require.Equal(t, []string{"one", "§atwo"}, build(t, `stick[lore=["one", {text:"two",color:green}]]`).Stack.Lore())

	// Leather dye, in each form.
	for _, form := range []string{"8388403", "0x7FFF33", "[0.498, 1.0, 0.2]", "{rgb:8388403}"} {
		h, ok := build(t, "leather_helmet[dyed_color="+form+"]").Stack.Item().(item.Helmet)
		require.True(t, ok)
		require.Equal(t, item.ArmourTierLeather{Colour: color.RGBA{0x7F, 0xFF, 0x33, 0xFF}}, h.Tier, form)
	}
	// Dye keeps enchantments the item would not normally take.
	h := build(t, "leather_boots[enchantments={sharpness:2},dyed_color=255]").Stack
	_, ok = h.Enchantment(enchantment.Sharpness)
	require.True(t, ok)

	// Blocks and Bedrock names.
	b = build(t, "stone")
	require.True(t, b.Block)
	require.Equal(t, "Stone", b.Name)
	name, _ := build(t, "minecraft:oak_log").Stack.Item().EncodeItem()
	require.Equal(t, "minecraft:oak_log", name)
	// A Bedrock data value: potion 5 is night vision.
	name, meta := build(t, "potion 1 5").Stack.Item().EncodeItem()
	require.Equal(t, "minecraft:potion", name)
	require.Equal(t, int16(5), meta)

	for text, want := range map[string]string{
		"nosuchitem":                                "Unknown item",
		"stone[nosuchcomponent=1]":                  "Unknown component 'minecraft:nosuchcomponent'",
		"stone[food={nutrition:1}]":                 "not supported",
		"stick[damage=1]":                           "no durability",
		"diamond_sword[damage=99999]":               "below the item's durability",
		"diamond_sword[damage=-1]":                  "non-negative",
		"diamond_sword[enchantments={x:1}]":         "Unknown enchantment 'x'",
		"diamond_sword[enchantments={sharpness:0}]": "1 to 255",
		"iron_helmet[dyed_color=1]":                 "leather",
		"stick[custom_name={color:red}]":            "only text",
		"stick[custom_name={text:a,color:nope}]":    "unknown colour",
		"stick[lore=\"one\"]":                       "list",
		"stick[unbreakable=1]":                      "expected {}",
	} {
		r, err := ParseRequest(text)
		require.NoError(t, err, text)
		_, err = Build(r)
		require.ErrorContains(t, err, want, text)
	}
}

func TestAfterTarget(t *testing.T) {
	require.Equal(t, `stone[lore=["a", "b"]] 2`, afterTarget(`@s stone[lore=["a", "b"]] 2`))
	require.Equal(t, "stone", afterTarget(`"Some Player" stone`))
	require.Equal(t, "", afterTarget("@a"))
}

func TestExplain(t *testing.T) {
	_, err := ParseRequest("stone[damage=1")
	require.True(t, strings.HasSuffix(explain(err, "stone[damage=1").Error(), "...e[damage=1<--[HERE]"), explain(err, "stone[damage=1").Error())
}

func TestOperators(t *testing.T) {
	id := uuid.MustParse("069a79f4-44e9-4726-a5be-fca90e38aaf5")
	o := NewOperators([]string{"Steve", "069a79f444e94726a5befca90e38aaf5", "2535400000000000", "  "})
	require.True(t, o.Includes("steve", false, uuid.Nil, ""))
	require.True(t, o.Includes("STEVE", true, uuid.New(), ""))
	require.True(t, o.Includes("Notch", true, id, ""))
	require.False(t, o.Includes("Notch", false, id, ""), "a UUID is a Java account")
	require.True(t, o.Includes("Bob", false, uuid.Nil, "2535400000000000"))
	require.False(t, o.Includes("Bob", true, uuid.Nil, "2535400000000000"), "an XUID is a Bedrock account")
	require.False(t, o.Includes("Alex", false, uuid.Nil, ""))
	require.False(t, NewOperators(nil).Includes("Steve", false, uuid.Nil, ""))
}

func TestReply(t *testing.T) {
	b := build(t, "diamond_sword")
	require.Equal(t, "Gave 2 [Diamond Sword] to Steve", Reply{Count: 2, Item: b, Target: "Steve", Targets: 1}.String())
	require.Equal(t, "Gave 2 [Diamond Sword] to 3 players", Reply{Count: 2, Item: b, Targets: 3}.String())
	j := Reply{Count: 2, Item: b, Target: "Steve", Targets: 1}.JavaText()
	require.Equal(t, "commands.give.success.single", j.Translate)
	require.Equal(t, "item.minecraft.diamond_sword", j.With[1].With[0].Translate)
	require.Equal(t, "block.minecraft.stone", Reply{Count: 1, Item: build(t, "stone"), Target: "x"}.JavaText().With[1].With[0].Translate)
}
