package java261

import (
	"sort"
	"strings"
	"sync"
)

// propBlock is one Java block's properties, from data/properties.gz
// (minecraft-data's pc blocks.json): the state IDs count them with the last
// varying fastest, from min.
type propBlock struct {
	name  string
	min   uint16
	props []propValues
	size  uint16 // how many states
}

type propValues struct {
	name   string
	values []string
	stride uint16 // states between one value and the next
}

var (
	propsOnce  sync.Once
	propBlocks []*propBlock // sorted by min
	propByName map[string]*propBlock
)

func loadProperties() {
	propsOnce.Do(func() {
		propByName = map[string]*propBlock{}
		for _, l := range lines("properties") {
			f := fields(l)
			b := &propBlock{name: f[0], min: uint16(atoi(f[1]))}
			for _, p := range f[2:] {
				k, v, _ := strings.Cut(p, "=")
				b.props = append(b.props, propValues{name: k, values: strings.Split(v, ",")})
			}
			stride := uint16(1)
			for i := len(b.props) - 1; i >= 0; i-- {
				b.props[i].stride = stride
				stride *= uint16(len(b.props[i].values))
			}
			b.size = stride
			propBlocks = append(propBlocks, b)
			propByName[b.name] = b
		}
		sort.Slice(propBlocks, func(i, j int) bool { return propBlocks[i].min < propBlocks[j].min })
	})
}

// blockOfState is the Java block a state belongs to, or nil for a block with
// no properties.
func blockOfState(state uint16) *propBlock {
	loadProperties()
	i := sort.Search(len(propBlocks), func(i int) bool { return propBlocks[i].min+propBlocks[i].size > state })
	if i == len(propBlocks) || propBlocks[i].min > state {
		return nil
	}
	return propBlocks[i]
}

func (b *propBlock) prop(name string) (propValues, bool) {
	for _, p := range b.props {
		if p.name == name {
			return p, true
		}
	}
	return propValues{}, false
}

// Property is the value of a property of a Java state, as minecraft-data
// writes it ("true", "north", "3"); ok is false if the block has no such
// property.
func Property(state uint16, name string) (value string, ok bool) {
	b := blockOfState(state)
	if b == nil {
		return "", false
	}
	p, ok := b.prop(name)
	if !ok {
		return "", false
	}
	return p.values[(state-b.min)/p.stride%uint16(len(p.values))], true
}

// WithProperty is a Java state with one property set to a value. ok is false,
// and the state is returned as it was, if the block has no such property or
// value.
func WithProperty(state uint16, name, value string) (uint16, bool) {
	b := blockOfState(state)
	if b == nil {
		return state, false
	}
	p, ok := b.prop(name)
	if !ok {
		return state, false
	}
	n := uint16(len(p.values))
	for i, v := range p.values {
		if v == value {
			cur := (state - b.min) / p.stride % n
			return state - cur*p.stride + uint16(i)*p.stride, true
		}
	}
	return state, false
}

// AsBlock is the state of another Java block ("minecraft:red_bed") with the
// property values of state: for blocks that differ only by name, such as the
// colours of a bed or banner. ok is false, and the state is returned as it
// was, unless both blocks have the same properties.
func AsBlock(state uint16, name string) (uint16, bool) {
	from := blockOfState(state)
	to := propByName[name]
	if from == nil || to == nil || len(from.props) != len(to.props) {
		return state, false
	}
	out := to.min
	for i, p := range from.props {
		q := to.props[i]
		if p.name != q.name || strings.Join(p.values, ",") != strings.Join(q.values, ",") {
			return state, false
		}
		out += (state - from.min) / p.stride % uint16(len(p.values)) * q.stride
	}
	return out, true
}

// BlockName is the Java block name of a state with properties ("minecraft:
// oak_fence"), or "" for a block without.
func BlockName(state uint16) string {
	if b := blockOfState(state); b != nil {
		return b.name
	}
	return ""
}

var (
	defaultsOnce  sync.Once
	defaultStates map[string]uint16
)

// DefaultState is a Java block's default state by name ("minecraft:cave_air"),
// from data/defaults.gz; false if Java has no such block.
func DefaultState(name string) (uint16, bool) {
	defaultsOnce.Do(func() {
		defaultStates = map[string]uint16{}
		for _, l := range lines("defaults") {
			f := fields(l)
			defaultStates[f[0]] = uint16(atoi(f[3]))
		}
	})
	s, ok := defaultStates[name]
	return s, ok
}
