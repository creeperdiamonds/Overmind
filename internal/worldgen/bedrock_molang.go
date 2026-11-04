package worldgen

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// The Molang that Bedrock's feature files carry in iterations, chances and coordinates: numbers,
// variable.* (originx/y/z, worldx/y/z, is_legacy), query.heightmap, query.above_top_solid,
// query.noise, query.has_biome_tag and the math.* functions they use, with ?:, ||, &&, !, the
// comparisons and + - * /. Bedrock evaluates Molang in float32; so does this. Expressions are
// parsed once when the data is read.

// molangEnv is what an expression reads.
type molangEnv struct {
	d                         *decoration
	r1                        *bmt // math.random's generator: the chunk's Random for a rule, the feature's in a nested scatter (T21).
	originX, originY, originZ float32
	worldX, worldY, worldZ    float32
	biome                     uint8
}

type mexpr func(e *molangEnv) float32

type mtok struct {
	kind byte // 'n' number, 's' string, 'i' identifier, 'o' operator
	s    string
	n    float32
}

func molangTokens(src string) ([]mtok, error) {
	s := strings.TrimRight(strings.TrimSpace(src), ";")
	var out []mtok
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c >= '0' && c <= '9' || c == '.' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9':
			j := i
			for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == '.') {
				j++
			}
			if j < len(s) && s[j] == 'f' {
				j++
			}
			v, err := strconv.ParseFloat(strings.TrimSuffix(s[i:j], "f"), 32)
			if err != nil {
				return nil, err
			}
			out = append(out, mtok{kind: 'n', n: float32(v)})
			i = j
		case c == '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return nil, fmt.Errorf("molang: open string in %q", src)
			}
			out = append(out, mtok{kind: 's', s: s[i+1 : i+1+j]})
			i += j + 2
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			j := i
			for j < len(s) && (s[j] == '_' || s[j] == '.' || s[j] >= 'a' && s[j] <= 'z' || s[j] >= 'A' && s[j] <= 'Z' || s[j] >= '0' && s[j] <= '9') {
				j++
			}
			out = append(out, mtok{kind: 'i', s: strings.ToLower(s[i:j])})
			i = j
		default:
			if i+1 < len(s) {
				if two := s[i : i+2]; two == "||" || two == "&&" || two == "==" || two == "!=" || two == "<=" || two == ">=" {
					out = append(out, mtok{kind: 'o', s: two})
					i += 2
					continue
				}
			}
			if !strings.ContainsRune("+-*/()?:<>!,", rune(c)) {
				return nil, fmt.Errorf("molang: %q in %q", c, src)
			}
			out = append(out, mtok{kind: 'o', s: string(c)})
			i++
		}
	}
	return out, nil
}

type mparser struct {
	t   []mtok
	i   int
	src string
	err error
	bd  *BedrockData
}

func (p *mparser) peek() mtok {
	if p.i < len(p.t) {
		return p.t[p.i]
	}
	return mtok{kind: 'e'}
}

func (p *mparser) isOp(s string) bool { t := p.peek(); return t.kind == 'o' && t.s == s }

func (p *mparser) fail(f string, a ...any) mexpr {
	if p.err == nil {
		p.err = fmt.Errorf("molang %q: %s", p.src, fmt.Sprintf(f, a...))
	}
	return func(*molangEnv) float32 { return 0 }
}

func b2f(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

func (p *mparser) expr() mexpr {
	c := p.or()
	if p.isOp("?") {
		p.i++
		a := p.expr()
		if !p.isOp(":") {
			return p.fail("no ':'")
		}
		p.i++
		b := p.expr()
		return func(e *molangEnv) float32 {
			if c(e) != 0 {
				return a(e)
			}
			return b(e)
		}
	}
	return c
}

func (p *mparser) or() mexpr {
	v := p.and()
	for p.isOp("||") {
		p.i++
		l, r := v, p.and()
		v = func(e *molangEnv) float32 { return b2f(l(e) != 0 || r(e) != 0) }
	}
	return v
}

func (p *mparser) and() mexpr {
	v := p.cmp()
	for p.isOp("&&") {
		p.i++
		l, r := v, p.cmp()
		v = func(e *molangEnv) float32 { return b2f(l(e) != 0 && r(e) != 0) }
	}
	return v
}

func (p *mparser) cmp() mexpr {
	v := p.add()
	for {
		t := p.peek()
		if t.kind != 'o' {
			return v
		}
		l := v
		switch t.s {
		case "==", "!=", "<", ">", "<=", ">=":
		default:
			return v
		}
		p.i++
		r := p.add()
		switch t.s {
		case "==":
			v = func(e *molangEnv) float32 { return b2f(l(e) == r(e)) }
		case "!=":
			v = func(e *molangEnv) float32 { return b2f(l(e) != r(e)) }
		case "<":
			v = func(e *molangEnv) float32 { return b2f(l(e) < r(e)) }
		case ">":
			v = func(e *molangEnv) float32 { return b2f(l(e) > r(e)) }
		case "<=":
			v = func(e *molangEnv) float32 { return b2f(l(e) <= r(e)) }
		case ">=":
			v = func(e *molangEnv) float32 { return b2f(l(e) >= r(e)) }
		}
	}
}

func (p *mparser) add() mexpr {
	v := p.mul()
	for p.isOp("+") || p.isOp("-") {
		minus := p.isOp("-")
		p.i++
		l, r := v, p.mul()
		if minus {
			v = func(e *molangEnv) float32 { return l(e) - r(e) }
		} else {
			v = func(e *molangEnv) float32 { return l(e) + r(e) }
		}
	}
	return v
}

func (p *mparser) mul() mexpr {
	v := p.unary()
	for p.isOp("*") || p.isOp("/") {
		div := p.isOp("/")
		p.i++
		l, r := v, p.unary()
		if div {
			v = func(e *molangEnv) float32 {
				d := r(e)
				if d == 0 {
					return 0
				}
				return l(e) / d
			}
		} else {
			v = func(e *molangEnv) float32 { return l(e) * r(e) }
		}
	}
	return v
}

func (p *mparser) args() []mexpr {
	p.i++ // (
	var out []mexpr
	if p.isOp(")") {
		p.i++
		return out
	}
	for {
		out = append(out, p.expr())
		if p.isOp(",") {
			p.i++
			continue
		}
		if !p.isOp(")") {
			p.fail("no ')'")
			return out
		}
		p.i++
		return out
	}
}

func (p *mparser) unary() mexpr {
	t := p.peek()
	switch {
	case t.kind == 'o' && t.s == "!":
		p.i++
		v := p.unary()
		return func(e *molangEnv) float32 { return b2f(v(e) == 0) }
	case t.kind == 'o' && t.s == "-":
		p.i++
		v := p.unary()
		return func(e *molangEnv) float32 { return -v(e) }
	case t.kind == 'o' && t.s == "(":
		p.i++
		v := p.expr()
		if !p.isOp(")") {
			return p.fail("no ')'")
		}
		p.i++
		return v
	case t.kind == 'n':
		p.i++
		n := t.n
		return func(*molangEnv) float32 { return n }
	case t.kind == 'i':
		p.i++
		name := t.s
		if strings.HasPrefix(name, "v.") {
			name = "variable." + name[2:]
		} else if strings.HasPrefix(name, "q.") {
			name = "query." + name[2:]
		}
		if p.isOp("(") {
			if name == "query.has_biome_tag" {
				p.i++
				s := p.peek()
				if s.kind != 's' {
					return p.fail("has_biome_tag wants a string")
				}
				p.i++
				if !p.isOp(")") {
					return p.fail("no ')'")
				}
				p.i++
				var set [2]uint64
				if p.bd != nil {
					set = p.bd.biomesWithTag(strings.TrimPrefix(s.s, "minecraft:"))
				}
				return func(e *molangEnv) float32 { return b2f(set[e.biome>>6]&(1<<(e.biome&63)) != 0) }
			}
			return p.call(name, p.args())
		}
		switch name {
		case "variable.originx":
			return func(e *molangEnv) float32 { return e.originX }
		case "variable.originz":
			return func(e *molangEnv) float32 { return e.originZ }
		case "variable.originy":
			return func(e *molangEnv) float32 { return e.originY }
		case "variable.worldx":
			return func(e *molangEnv) float32 { return e.worldX }
		case "variable.worldy":
			return func(e *molangEnv) float32 { return e.worldY }
		case "variable.worldz":
			return func(e *molangEnv) float32 { return e.worldZ }
		case "variable.is_legacy":
			return func(*molangEnv) float32 { return 0 } // A world made by this version.
		}
		return p.fail("unknown %s", name)
	}
	return p.fail("token %v", t)
}

func (p *mparser) call(name string, a []mexpr) mexpr {
	need := func(n int) bool {
		if len(a) != n {
			p.fail("%s takes %d arguments", name, n)
			return false
		}
		return true
	}
	switch name {
	case "query.heightmap":
		if !need(2) {
			break
		}
		return func(e *molangEnv) float32 { return float32(e.d.bedrockHeight(int(a[0](e)), int(a[1](e)))) }
	case "query.above_top_solid":
		if !need(2) {
			break
		}
		return func(e *molangEnv) float32 { return float32(e.d.aboveTopSolid(int(a[0](e)), int(a[1](e)))) }
	case "query.noise":
		if !need(2) {
			break
		}
		return func(e *molangEnv) float32 { return queryNoise(a[0](e), a[1](e)) }
	case "math.ceil":
		if !need(1) {
			break
		}
		return func(e *molangEnv) float32 { return float32(math.Ceil(float64(a[0](e)))) }
	case "math.floor":
		if !need(1) {
			break
		}
		return func(e *molangEnv) float32 { return float32(math.Floor(float64(a[0](e)))) }
	case "math.trunc":
		if !need(1) {
			break
		}
		return func(e *molangEnv) float32 { return float32(math.Trunc(float64(a[0](e)))) }
	case "math.round":
		if !need(1) {
			break
		}
		return func(e *molangEnv) float32 { return float32(math.RoundToEven(float64(a[0](e)))) }
	case "math.abs":
		if !need(1) {
			break
		}
		return func(e *molangEnv) float32 { return float32(math.Abs(float64(a[0](e)))) }
	case "math.sqrt":
		if !need(1) {
			break
		}
		return func(e *molangEnv) float32 { return float32(math.Sqrt(float64(a[0](e)))) }
	case "math.max":
		if !need(2) {
			break
		}
		return func(e *molangEnv) float32 { return max(a[0](e), a[1](e)) }
	case "math.min":
		if !need(2) {
			break
		}
		return func(e *molangEnv) float32 { return min(a[0](e), a[1](e)) }
	case "math.clamp":
		if !need(3) {
			break
		}
		return func(e *molangEnv) float32 { return min(max(a[0](e), a[1](e)), a[2](e)) }
	case "math.random":
		if !need(2) {
			break
		}
		return func(e *molangEnv) float32 {
			lo, hi := a[0](e), a[1](e)
			return lo + e.r1.nextFloat()*(hi-lo)
		}
	case "math.random_integer":
		if !need(2) {
			break
		}
		return func(e *molangEnv) float32 {
			lo, hi := int32(a[0](e)), int32(a[1](e))
			return float32(lo + e.r1.nextIntN(hi-lo+1))
		}
	default:
		return p.fail("unknown function %s", name)
	}
	return p.fail("bad call")
}

// parseMolang compiles an expression.
func parseMolang(src string, bd *BedrockData) (mexpr, error) {
	t, err := molangTokens(src)
	if err != nil {
		return nil, err
	}
	p := &mparser{t: t, src: src, bd: bd}
	f := p.expr()
	if p.err == nil && p.i != len(p.t) {
		p.err = fmt.Errorf("molang %q: trailing tokens", src)
	}
	if p.err != nil {
		return nil, p.err
	}
	return f, nil
}

// queryNoise is query.noise (S5): Random(2345)'s one-octave 2D simplex, no offsets.
func queryNoise(a, b float32) float32 { return queryNoiseOctave.at2(a, b) * 70 }

var queryNoiseOctave = oneOctaveSimplex(2345)

// oneOctaveSimplex is the one-octave simplex Bedrock builds from Random(seed) (query.noise and the
// freeze pass's temperature).
func oneOctaveSimplex(seed uint32) *bSimplex {
	m := newBMT(seed)
	st := newBSimplexStack(1, m.nextFloat, func(n uint32) int32 { return int32(m.u32() % n) }, true)
	return &st.oct[0]
}
