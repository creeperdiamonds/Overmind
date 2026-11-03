package give

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// SNBT values as parseSNBT returns them: a compound is a Compound, a list or
// typed array a []any, a string a string, true and false a bool, any whole
// number an int64 and any other number a float64. The number's type suffix
// (b, s, l, f, d) is read and dropped: the components /give understands want a
// whole number or a fraction, never a particular width.

// Compound is an SNBT compound with its keys in the order they were written.
type Compound struct {
	Keys   []string
	Values map[string]any
}

// Get returns the value of a key.
func (c Compound) Get(k string) (any, bool) { v, ok := c.Values[k]; return v, ok }

// SyntaxError is where and why the command text could not be read. Pos is a
// byte offset into the text given to the parser.
type SyntaxError struct {
	Pos int
	Msg string
}

func (e *SyntaxError) Error() string { return e.Msg }

// snbt reads SNBT, the text form of NBT that item components are written in
// (minecraft.wiki "NBT format", SNBT section, and "Data component format",
// oldid 3852498). It reads Java 1.21.5+'s form, which takes the older forms
// too: quoted strings with either quote and backslash escapes, unquoted
// strings, true and false, numbers with an optional sign, type suffix,
// underscores, hexadecimal (0x) and binary (0b), lists, typed arrays ([B;
// ...], [I; ...], [L; ...]) and compounds.
type snbt struct {
	s string
	i int
}

// maxDepth is how deeply lists and compounds may nest, as NBT's own limit.
const maxDepth = 512

func (p *snbt) fail(format string, a ...any) error {
	return &SyntaxError{Pos: p.i, Msg: fmt.Sprintf(format, a...)}
}

func (p *snbt) space() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t' || p.s[p.i] == '\n' || p.s[p.i] == '\r') {
		p.i++
	}
}

func (p *snbt) peek() byte {
	if p.i < len(p.s) {
		return p.s[p.i]
	}
	return 0
}

// value reads one value, after any whitespace.
func (p *snbt) value(depth int) (any, error) {
	if depth > maxDepth {
		return nil, p.fail("NBT nested too deeply")
	}
	p.space()
	switch c := p.peek(); c {
	case 0:
		return nil, p.fail("Expected value")
	case '{':
		return p.compound(depth)
	case '[':
		return p.list(depth)
	case '"', '\'':
		return p.quoted()
	default:
		start := p.i
		word := p.unquoted()
		if word == "" {
			return nil, p.fail("Expected value")
		}
		return wordValue(word, start)
	}
}

func (p *snbt) compound(depth int) (any, error) {
	p.i++ // {
	c := Compound{Values: map[string]any{}}
	p.space()
	if p.peek() == '}' {
		p.i++
		return c, nil
	}
	for {
		p.space()
		keyAt := p.i
		var key string
		if q := p.peek(); q == '"' || q == '\'' {
			k, err := p.quoted()
			if err != nil {
				return nil, err
			}
			key = k
		} else {
			key = p.unquoted()
			if key == "" {
				return nil, p.fail("Expected key")
			}
		}
		if _, dup := c.Values[key]; dup {
			return nil, &SyntaxError{Pos: keyAt, Msg: fmt.Sprintf("Key '%s' is repeated", key)}
		}
		p.space()
		if p.peek() != ':' {
			return nil, p.fail("Expected ':'")
		}
		p.i++
		v, err := p.value(depth + 1)
		if err != nil {
			return nil, err
		}
		c.Keys = append(c.Keys, key)
		c.Values[key] = v
		p.space()
		switch p.peek() {
		case ',':
			p.i++
		case '}':
			p.i++
			return c, nil
		default:
			return nil, p.fail("Expected ',' or '}'")
		}
	}
}

func (p *snbt) list(depth int) (any, error) {
	p.i++ // [
	out := []any{}
	// A typed array: B, I or L, then a semicolon.
	if p.i+1 < len(p.s) && p.s[p.i+1] == ';' && strings.IndexByte("BILbil", p.s[p.i]) >= 0 {
		p.i += 2
	}
	p.space()
	if p.peek() == ']' {
		p.i++
		return out, nil
	}
	for {
		v, err := p.value(depth + 1)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		p.space()
		switch p.peek() {
		case ',':
			p.i++
		case ']':
			p.i++
			return out, nil
		default:
			return nil, p.fail("Expected ',' or ']'")
		}
	}
}

// quoted reads a string in single or double quotes. A backslash escapes the
// quotes and itself, and since 1.21.5 also gives \b \f \n \r \s \t, \xHH,
// \uHHHH and \UHHHHHHHH.
func (p *snbt) quoted() (string, error) {
	q := p.s[p.i]
	p.i++
	var b strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c == q:
			p.i++
			return b.String(), nil
		case c == '\\':
			p.i++
			if p.i >= len(p.s) {
				return "", p.fail("Unclosed quoted string")
			}
			e := p.s[p.i]
			p.i++
			switch e {
			case '\\', '"', '\'':
				b.WriteByte(e)
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 's':
				b.WriteByte(' ')
			case 't':
				b.WriteByte('\t')
			case 'x', 'u', 'U':
				n := map[byte]int{'x': 2, 'u': 4, 'U': 8}[e]
				if p.i+n > len(p.s) {
					return "", p.fail("Bad escape \\%c", e)
				}
				r, err := strconv.ParseUint(p.s[p.i:p.i+n], 16, 32)
				if err != nil || !utf8.ValidRune(rune(r)) {
					return "", p.fail("Bad escape \\%c%s", e, p.s[p.i:p.i+n])
				}
				b.WriteRune(rune(r))
				p.i += n
			default:
				p.i -= 2
				return "", p.fail("Invalid escape sequence '\\%c' in quoted string", e)
			}
		default:
			b.WriteByte(c)
			p.i++
		}
	}
	return "", p.fail("Unclosed quoted string")
}

// unquoted reads the characters an unquoted string or number may hold.
func (p *snbt) unquoted() string {
	start := p.i
	for p.i < len(p.s) && unquotedChar(p.s[p.i]) {
		p.i++
	}
	return p.s[start:p.i]
}

func unquotedChar(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c == '-' || c == '.' || c == '+'
}

// wordValue is an unquoted word as a value: true, false, a number, or else a
// string. A word that starts like a number but is not one is an error, as in
// the game ("1.2.3" is not a string).
func wordValue(w string, at int) (any, error) {
	switch strings.ToLower(w) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	if v, ok := number(w); ok {
		return v, nil
	}
	if c := w[0]; c >= '0' && c <= '9' || (c == '-' || c == '+' || c == '.') && len(w) > 1 {
		return nil, &SyntaxError{Pos: at, Msg: fmt.Sprintf("Invalid number '%s'", w)}
	}
	return w, nil
}

// number reads a whole number or fraction with SNBT's optional suffixes.
func number(w string) (any, bool) {
	s := strings.ReplaceAll(w, "_", "")
	if s == "" || strings.HasPrefix(w, "_") || strings.HasSuffix(w, "_") {
		return nil, false
	}
	neg := false
	body := s
	if body[0] == '+' || body[0] == '-' {
		neg = body[0] == '-'
		body = body[1:]
	}
	lower := strings.ToLower(body)
	// Hexadecimal and binary are whole numbers only: their suffix may carry a
	// signedness letter (s or u) before the width.
	if base := map[string]int{"0x": 16, "0b": 2}[lower[:min(2, len(lower))]]; base != 0 {
		digits := strings.TrimRight(lower[2:], "bsilu")
		if base == 16 {
			// b is a hex digit; only a trailing s/u then width counts as suffix.
			digits = hexDigits(lower[2:])
		}
		v, err := strconv.ParseInt(digits, base, 64)
		if err != nil || digits == "" {
			return nil, false
		}
		if neg {
			v = -v
		}
		return v, true
	}
	// Whole numbers: a suffix b, s, i or l, with an optional s or u before it.
	whole := lower
	if n := len(whole); n > 0 && strings.IndexByte("bsil", whole[n-1]) >= 0 {
		whole = whole[:n-1]
		if n := len(whole); n > 0 && (whole[n-1] == 'u' || whole[n-1] == 's') {
			whole = whole[:n-1]
		}
	}
	if whole != "" && strings.Trim(whole, "0123456789") == "" {
		v, err := strconv.ParseInt(whole, 10, 64)
		if err != nil {
			return nil, false
		}
		if neg {
			v = -v
		}
		return v, true
	}
	// Fractions: an optional f or d.
	frac := lower
	if n := len(frac); n > 0 && (frac[n-1] == 'f' || frac[n-1] == 'd') {
		frac = frac[:n-1]
	}
	if frac == "" || strings.Trim(frac, "0123456789.e+-") != "" || strings.Trim(frac, ".e+-") == "" {
		return nil, false
	}
	v, err := strconv.ParseFloat(frac, 64)
	if err != nil {
		return nil, false
	}
	if neg {
		v = -v
	}
	return v, true
}

// hexDigits drops a hexadecimal number's suffix: s or u then a width letter
// (0xFFub, 0x10sl). A bare trailing b is the digit eleven.
func hexDigits(s string) string {
	if n := len(s); n >= 2 && (s[n-2] == 'u' || s[n-2] == 's') && strings.IndexByte("bsil", s[n-1]) >= 0 {
		return s[:n-2]
	}
	if n := len(s); n >= 1 && strings.IndexByte("sil", s[n-1]) >= 0 {
		return s[:n-1]
	}
	return s
}

// ParseSNBT reads one SNBT value that must fill the whole text.
func ParseSNBT(s string) (any, error) {
	p := &snbt{s: s}
	v, err := p.value(0)
	if err != nil {
		return nil, err
	}
	p.space()
	if p.i != len(p.s) {
		return nil, p.fail("Unexpected text after the value")
	}
	return v, nil
}
