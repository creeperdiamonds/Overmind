package give

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Request is what /give was asked for, after the targets: the item, its
// components, how many, and Bedrock's data value.
type Request struct {
	// Item is the item ID as written, without minecraft:.
	Item string
	// Components in the order written.
	Components []Component
	// Count is how many to give, 1 when not written.
	Count int
	// Data is Bedrock's data value (the item's metadata), -1 when not written.
	Data int
}

// Component is one entry of an item's component list: item[name=value].
type Component struct {
	Name  string // without minecraft:
	Value any    // as parseSNBT reads it
	Pos   int    // where it starts in the text
}

// ParseRequest reads the part of a /give command after its targets, in either
// edition's order (minecraft.wiki "Commands/give", oldid 3855978):
//
//	Java:    <item>[<components>] [<count>]
//	Bedrock: <itemName> [amount] [data] [components]
//
// The item is a namespaced ID, minecraft: optional, followed by components in
// Java 1.20.5+'s form: [name=SNBT, ...]. Bedrock's JSON components (can_destroy
// and the like) are refused with an error, as Dragonfly has nothing to hold
// them.
func ParseRequest(text string) (Request, error) {
	r := Request{Count: 1, Data: -1}
	p := &snbt{s: text}
	p.space()
	start := p.i
	for p.i < len(p.s) && idChar(p.s[p.i]) {
		p.i++
	}
	id := p.s[start:p.i]
	if id == "" {
		return r, p.fail("Expected an item")
	}
	ns, name, found := strings.Cut(id, ":")
	if !found {
		ns, name = "minecraft", id
	}
	if ns != "minecraft" || name == "" || strings.Contains(name, ":") {
		return r, &SyntaxError{Pos: start, Msg: fmt.Sprintf("Unknown item '%s'", id)}
	}
	r.Item = name
	if p.peek() == '[' {
		comps, err := components(p)
		if err != nil {
			return r, err
		}
		r.Components = comps
	}
	if p.i < len(p.s) && p.s[p.i] != ' ' {
		return r, p.fail("Expected whitespace to end one argument, but found trailing data")
	}
	// Then up to two whole numbers: the count, and Bedrock's data value.
	for n := 0; ; n++ {
		p.space()
		if p.i == len(p.s) {
			return r, nil
		}
		if c := p.peek(); c == '{' || c == '[' {
			return r, p.fail("Bedrock's JSON item components are not supported; use Java's item[component=value] form")
		}
		at := p.i
		word := p.word()
		if n >= 2 {
			return r, &SyntaxError{Pos: at, Msg: "Incorrect argument for command"}
		}
		v, err := strconv.ParseInt(word, 10, 64)
		if err != nil {
			return r, &SyntaxError{Pos: at, Msg: fmt.Sprintf("Invalid integer '%s'", word)}
		}
		if n == 0 {
			if v < 1 {
				return r, &SyntaxError{Pos: at, Msg: fmt.Sprintf("Integer must not be less than 1, found %d", v)}
			}
			if v > math.MaxInt32 {
				return r, &SyntaxError{Pos: at, Msg: fmt.Sprintf("Integer must not be more than %d, found %d", math.MaxInt32, v)}
			}
			r.Count = int(v)
		} else {
			if v < 0 || v > math.MaxInt16 {
				return r, &SyntaxError{Pos: at, Msg: fmt.Sprintf("Data value must be from 0 to %d, found %d", math.MaxInt16, v)}
			}
			r.Data = int(v)
		}
	}
}

// word reads up to the next space.
func (p *snbt) word() string {
	start := p.i
	for p.i < len(p.s) && p.s[p.i] != ' ' {
		p.i++
	}
	return p.s[start:p.i]
}

// idChar is a character of a resource location.
func idChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' || c == '/' || c == ':' ||
		c >= 'A' && c <= 'Z' // read so that the error names it, refused below
}

// components reads [name=value, ...]. A name may appear once. "!name", which
// removes a default component, is refused: a given item starts with its
// defaults and Dragonfly cannot remove them.
func components(p *snbt) ([]Component, error) {
	p.i++ // [
	var out []Component
	seen := map[string]bool{}
	p.space()
	if p.peek() == ']' {
		p.i++
		return out, nil
	}
	for {
		p.space()
		at := p.i
		if p.peek() == '!' {
			return nil, p.fail("Removing a component (!name) is not supported")
		}
		for p.i < len(p.s) && idChar(p.s[p.i]) {
			p.i++
		}
		id := p.s[at:p.i]
		if id == "" {
			return nil, p.fail("Expected a component name")
		}
		name := strings.TrimPrefix(id, "minecraft:")
		if seen[name] {
			return nil, &SyntaxError{Pos: at, Msg: fmt.Sprintf("Component 'minecraft:%s' was repeated, but only one value can be specified", name)}
		}
		seen[name] = true
		p.space()
		if p.peek() != '=' {
			return nil, p.fail("Expected '=' after component 'minecraft:%s'", name)
		}
		p.i++
		v, err := p.value(0)
		if err != nil {
			return nil, err
		}
		out = append(out, Component{Name: name, Value: v, Pos: at})
		p.space()
		switch p.peek() {
		case ',':
			p.i++
		case ']':
			p.i++
			return out, nil
		default:
			return nil, p.fail("Expected ',' or ']' in the component list")
		}
	}
}
