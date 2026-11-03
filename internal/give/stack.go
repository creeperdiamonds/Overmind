package give

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"

	"github.com/creeperdiamonds/overmind/internal/tables/java261"
)

// Built is a Request made into an item: one of it, to be grown to the count,
// and how it is named in the reply.
type Built struct {
	Stack item.Stack
	// JavaName is the item's Java ID without minecraft:, "" for an item Java
	// does not have; Block tells whether Java names it as a block.
	JavaName string
	Block    bool
	// Name is the item as the reply names it in English: its custom name, or its
	// ID made readable.
	Name string
}

// Build finds the item, by its Java ID first and then its Bedrock one, and
// applies the components. A Bedrock data value picks the Bedrock item of that
// metadata.
func Build(r Request) (Built, error) {
	name := strings.ToLower(r.Item)
	var b Built
	var it world.Item
	found := false
	if id := java261.JavaItemID(name); id >= 0 && r.Data < 0 {
		if bn, meta, ok := java261.BedrockItem(id); ok {
			it, found = world.ItemByName(bn, meta)
		}
		if !found {
			return b, fmt.Errorf("Item 'minecraft:%s' is not in this server yet", name)
		}
		b.JavaName = name
	} else {
		meta := int16(max(r.Data, 0))
		if it, found = world.ItemByName("minecraft:"+name, meta); !found {
			return b, fmt.Errorf("Unknown item 'minecraft:%s'", name)
		}
		// The Java item it shows as, for the reply.
		if id, ok := java261.JavaItem("minecraft:"+name, meta); ok {
			b.JavaName = java261.JavaItemName(id)
		}
	}
	if bn, _ := it.EncodeItem(); bn == "minecraft:air" {
		return b, fmt.Errorf("Unknown item 'minecraft:%s'", name)
	}
	if b.JavaName != "" {
		_, b.Block = java261.DefaultState("minecraft:" + b.JavaName)
	}
	s := item.NewStack(it, 1)
	for _, c := range r.Components {
		var err error
		if s, err = apply(s, c); err != nil {
			return b, err
		}
	}
	b.Stack = s
	b.Name = s.CustomName()
	if b.Name == "" {
		readable := b.JavaName
		if readable == "" {
			readable = name
		}
		b.Name = titleCase(readable)
	}
	return b, nil
}

// titleCase turns diamond_sword into Diamond Sword.
func titleCase(id string) string {
	words := strings.Split(id, "_")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// Supported lists the components /give can apply, as Dragonfly items hold
// them.
var Supported = []string{"custom_name", "lore", "enchantments", "damage", "unbreakable", "dyed_color"}

// apply sets one component on the stack, or says why it cannot.
func apply(s item.Stack, c Component) (item.Stack, error) {
	fail := func(format string, a ...any) (item.Stack, error) {
		return s, fmt.Errorf("Malformed 'minecraft:%s' component: %s", c.Name, fmt.Sprintf(format, a...))
	}
	switch c.Name {
	case "custom_name":
		t, err := legacyText(c.Value)
		if err != nil {
			return fail("%v", err)
		}
		return s.WithCustomName(t), nil
	case "lore":
		list, ok := c.Value.([]any)
		if !ok {
			return fail("expected a list of text components")
		}
		if len(list) > 256 {
			return fail("at most 256 lines")
		}
		lines := make([]string, 0, len(list))
		for _, l := range list {
			t, err := legacyText(l)
			if err != nil {
				return fail("%v", err)
			}
			lines = append(lines, t)
		}
		return s.WithLore(lines...), nil
	case "enchantments":
		m, ok := c.Value.(Compound)
		if !ok {
			return fail("expected {name:level, ...}")
		}
		// 1.20.5 to 1.21.4 wrote {levels:{...}, show_in_tooltip:...}.
		if inner, ok := m.Get("levels"); ok {
			if m, ok = inner.(Compound); !ok {
				return fail("expected levels:{name:level, ...}")
			}
		}
		var list []item.Enchantment
		for _, k := range m.Keys {
			t, ok := enchantmentByJavaName(strings.TrimPrefix(k, "minecraft:"))
			if !ok {
				return s, fmt.Errorf("Unknown enchantment '%s'", k)
			}
			lvl, ok := m.Values[k].(int64)
			if !ok || lvl < 1 || lvl > 255 {
				return fail("level of %s must be a whole number from 1 to 255", k)
			}
			list = append(list, item.NewEnchantment(t, int(lvl)))
		}
		// As vanilla's /give: any enchantment on any item, together.
		return s.WithForcedEnchantments(list...), nil
	case "damage":
		d, ok := c.Value.(int64)
		if !ok || d < 0 {
			return fail("expected a non-negative whole number")
		}
		max := s.MaxDurability()
		if max <= 0 {
			return s, fmt.Errorf("Item cannot take damage: it has no durability")
		}
		if d >= int64(max) {
			return fail("must be below the item's durability, %d", max)
		}
		return s.WithDurability(max - int(d)), nil
	case "unbreakable":
		if _, ok := c.Value.(Compound); !ok {
			return fail("expected {}")
		}
		return s.AsUnbreakable(), nil
	case "dyed_color":
		rgb, ok := dyedColour(c.Value)
		if !ok {
			return fail("expected a colour as a whole number (0xRRGGBB), [r, g, b] from 0.0 to 1.0, or {rgb:...}")
		}
		col := color.RGBA{R: uint8(rgb >> 16), G: uint8(rgb >> 8), B: uint8(rgb), A: 0xFF}
		leather := item.ArmourTierLeather{Colour: col}
		// WithItem keeps only enchantments the item would take; /give keeps all.
		recolour := func(it world.Item) item.Stack { return s.WithItem(it).WithForcedEnchantments(s.Enchantments()...) }
		switch it := s.Item().(type) {
		case item.Helmet:
			if _, ok := it.Tier.(item.ArmourTierLeather); ok {
				it.Tier = leather
				return recolour(it), nil
			}
		case item.Chestplate:
			if _, ok := it.Tier.(item.ArmourTierLeather); ok {
				it.Tier = leather
				return recolour(it), nil
			}
		case item.Leggings:
			if _, ok := it.Tier.(item.ArmourTierLeather); ok {
				it.Tier = leather
				return recolour(it), nil
			}
		case item.Boots:
			if _, ok := it.Tier.(item.ArmourTierLeather); ok {
				it.Tier = leather
				return recolour(it), nil
			}
		}
		return s, fmt.Errorf("Component 'minecraft:dyed_color' needs leather armour")
	}
	if java261.ComponentType(c.Name) >= 0 {
		return s, fmt.Errorf("Component 'minecraft:%s' is not supported; /give takes %s", c.Name, strings.Join(Supported, ", "))
	}
	return s, fmt.Errorf("Unknown component 'minecraft:%s'", c.Name)
}

// dyedColour reads dyed_color: an int, [r, g, b] in 0..1, or the older
// {rgb:int, show_in_tooltip:...}.
func dyedColour(v any) (int, bool) {
	switch v := v.(type) {
	case int64:
		return int(v) & 0xFFFFFF, true
	case []any:
		if len(v) != 3 {
			return 0, false
		}
		rgb := 0
		for _, e := range v {
			var f float64
			switch e := e.(type) {
			case float64:
				f = e
			case int64:
				f = float64(e)
			default:
				return 0, false
			}
			if f < 0 || f > 1 {
				return 0, false
			}
			rgb = rgb<<8 | int(f*255+0.5)
		}
		return rgb, true
	case Compound:
		if c, ok := v.Get("rgb"); ok {
			return dyedColour(c)
		}
	}
	return 0, false
}

// enchantmentByJavaName finds a Dragonfly enchantment by its Java ID: "Fire
// Aspect" is fire_aspect, and a curse is named the other way round ("Curse of
// Vanishing" is vanishing_curse), as the Java front end names them.
func enchantmentByJavaName(key string) (item.EnchantmentType, bool) {
	for _, t := range item.Enchantments() {
		k := strings.ReplaceAll(strings.ToLower(t.Name()), " ", "_")
		if rest, ok := strings.CutPrefix(k, "curse_of_"); ok {
			k = rest + "_curse"
		}
		if k == key {
			return t, true
		}
	}
	return nil, false
}
