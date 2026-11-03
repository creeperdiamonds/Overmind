package give

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Item names and lore are text components in Java (minecraft.wiki "Text
// component format") and a string with § formatting codes in Dragonfly and
// Bedrock. legacyText turns a component, given as SNBT (1.21.5+) or as a
// string of JSON (1.20.5 to 1.21.4, custom_name='"Blade"'), into such a string.
// The Java front end shows it to Java players as it is: the Java client draws §
// codes in literal text too.

// colourCodes are the sixteen named colours and their § codes.
var colourCodes = map[string]byte{
	"black": '0', "dark_blue": '1', "dark_green": '2', "dark_aqua": '3', "dark_red": '4', "dark_purple": '5',
	"gold": '6', "gray": '7', "dark_gray": '8', "blue": '9', "green": 'a', "aqua": 'b', "red": 'c',
	"light_purple": 'd', "yellow": 'e', "white": 'f',
}

// colourRGB are the same colours' values, to find the nearest for a hex colour.
var colourRGB = map[byte]int{
	'0': 0x000000, '1': 0x0000AA, '2': 0x00AA00, '3': 0x00AAAA, '4': 0xAA0000, '5': 0xAA00AA, '6': 0xFFAA00,
	'7': 0xAAAAAA, '8': 0x555555, '9': 0x5555FF, 'a': 0x55FF55, 'b': 0x55FFFF, 'c': 0xFF5555, 'd': 0xFF55FF,
	'e': 0xFFFF55, 'f': 0xFFFFFF,
}

// formatCodes are the style flags and their § codes, in the order written.
var formatCodes = []struct {
	key  string
	code byte
}{{"obfuscated", 'k'}, {"bold", 'l'}, {"strikethrough", 'm'}, {"underlined", 'n'}, {"italic", 'o'}}

// style is a component's colour and flags as inherited by its children.
type style struct {
	colour byte // 0 for none
	flags  map[byte]bool
}

func (s style) codes() string {
	var b strings.Builder
	if s.colour != 0 {
		b.WriteString("§" + string(s.colour))
	}
	for _, f := range formatCodes {
		if s.flags[f.code] {
			b.WriteString("§" + string(f.code))
		}
	}
	return b.String()
}

// legacyText is a text component as a § string. A plain string is the text
// itself unless it is JSON text, which is read as such.
func legacyText(v any) (string, error) {
	if s, ok := v.(string); ok {
		if j, ok := jsonText(s); ok {
			v = j
		}
	}
	var segs []segment
	if err := flatten(v, style{}, &segs, 0); err != nil {
		return "", err
	}
	var b strings.Builder
	cur := ""
	for _, sg := range segs {
		if sg.codes != cur {
			if cur != "" {
				b.WriteString("§r")
			}
			b.WriteString(sg.codes)
			cur = sg.codes
		}
		b.WriteString(sg.text)
	}
	return b.String(), nil
}

// jsonText reads a string holding a JSON text component: a JSON string, object
// or array.
func jsonText(s string) (any, bool) {
	t := strings.TrimSpace(s)
	if t == "" || !strings.ContainsRune(`"{[`, rune(t[0])) || !json.Valid([]byte(t)) {
		return nil, false
	}
	var v any
	if json.Unmarshal([]byte(t), &v) != nil {
		return nil, false
	}
	return fromJSON(v), true
}

// fromJSON turns decoded JSON into the shapes parseSNBT gives.
func fromJSON(v any) any {
	switch v := v.(type) {
	case map[string]any:
		c := Compound{Values: map[string]any{}}
		for k, e := range v {
			c.Keys = append(c.Keys, k)
			c.Values[k] = fromJSON(e)
		}
		return c
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = fromJSON(e)
		}
		return out
	}
	return v
}

type segment struct{ codes, text string }

// flatten walks a component and its extra children in order.
func flatten(v any, parent style, out *[]segment, depth int) error {
	if depth > maxDepth {
		return fmt.Errorf("text component nested too deeply")
	}
	switch v := v.(type) {
	case string:
		*out = append(*out, segment{parent.codes(), v})
		return nil
	case int64, float64, bool:
		*out = append(*out, segment{parent.codes(), scalarText(v)})
		return nil
	case []any:
		// Each entry in turn. (In Java the rest also take the first entry's
		// style; here they take only the list's own.)
		if len(v) == 0 {
			return fmt.Errorf("an empty list is not a text component")
		}
		for _, e := range v {
			if err := flatten(e, parent, out, depth+1); err != nil {
				return err
			}
		}
		return nil
	case Compound:
		st := style{colour: parent.colour, flags: map[byte]bool{}}
		for k, on := range parent.flags {
			st.flags[k] = on
		}
		if c, ok := v.Get("color"); ok {
			name, _ := c.(string)
			code, err := colourCode(name)
			if err != nil {
				return err
			}
			st.colour = code
		}
		for _, f := range formatCodes {
			if b, ok := v.Get(f.key); ok {
				on, ok := truth(b)
				if !ok {
					return fmt.Errorf("'%s' in a text component must be true or false", f.key)
				}
				st.flags[f.code] = on
			}
		}
		text, err := ownText(v)
		if err != nil {
			return err
		}
		*out = append(*out, segment{st.codes(), text})
		if extra, ok := v.Get("extra"); ok {
			list, ok := extra.([]any)
			if !ok {
				return fmt.Errorf("'extra' in a text component must be a list")
			}
			for _, e := range list {
				if err := flatten(e, st, out, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return fmt.Errorf("not a text component")
}

// ownText is a compound component's own content. Only literal text can be
// shown by a server that has no client language files: a translation shows its
// fallback, or its key.
func ownText(c Compound) (string, error) {
	if t, ok := c.Get("text"); ok {
		return scalarText(t), nil
	}
	if t, ok := c.Get("translate"); ok {
		if f, ok := c.Get("fallback"); ok {
			return scalarText(f), nil
		}
		return scalarText(t), nil
	}
	if _, ok := c.Get("extra"); ok {
		return "", nil
	}
	return "", fmt.Errorf("only text and translate text components are supported")
}

func scalarText(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return fmt.Sprint(v)
}

// truth reads a boolean as SNBT may write it: true/false or 1b/0b.
func truth(v any) (bool, bool) {
	switch v := v.(type) {
	case bool:
		return v, true
	case int64:
		return v != 0, v == 0 || v == 1
	}
	return false, false
}

// colourCode is a named colour's code, or for "#RRGGBB" the nearest of the
// sixteen: Bedrock has no other colours.
func colourCode(name string) (byte, error) {
	if c, ok := colourCodes[name]; ok {
		return c, nil
	}
	if h, ok := strings.CutPrefix(name, "#"); ok && len(h) == 6 {
		if rgb, err := strconv.ParseUint(h, 16, 32); err == nil {
			return nearestColour(int(rgb)), nil
		}
	}
	return 0, fmt.Errorf("unknown colour '%s'", name)
}

func nearestColour(rgb int) byte {
	best, bestD := byte('f'), -1
	for code, c := range colourRGB {
		dr, dg, db := (rgb>>16&0xFF)-(c>>16&0xFF), (rgb>>8&0xFF)-(c>>8&0xFF), (rgb&0xFF)-(c&0xFF)
		if d := dr*dr + dg*dg + db*db; bestD < 0 || d < bestD || d == bestD && code < best {
			best, bestD = code, d
		}
	}
	return best
}
