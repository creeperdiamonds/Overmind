// Package give is Overmind's /give, for both editions' players: Java's 1.20.5+
// item syntax with components, Bedrock's argument order, the selectors @s, @p,
// @a and @r, and the Operators setting for who may use it.
package give

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"github.com/creeperdiamonds/overmind/internal/javaproto"
)

var registerOnce sync.Once

// Register adds /give to Dragonfly's commands, once per process.
func Register() {
	registerOnce.Do(func() {
		cmd.Register(cmd.New("give", "Gives an item to a player.", nil, typed{}, raw{}))
	})
}

// typed is /give as Bedrock clients show it: a target, an item and an amount.
// It is tried first and runs when the line splits into those three.
type typed struct {
	Targets []cmd.Target      `cmd:"player"`
	Item    string            `cmd:"item"`
	Amount  cmd.Optional[int] `cmd:"amount"`
}

// raw takes everything after the targets, for components with spaces in them
// and Bedrock's data value.
type raw struct {
	Targets []cmd.Target `cmd:"player"`
	Item    cmd.Varargs  `cmd:"item"`
}

func (typed) Allow(src cmd.Source) bool { return allowed(src) }
func (raw) Allow(src cmd.Source) bool   { return allowed(src) }

// allowed reports whether the source is a player the server made an operator.
// Anyone else is told the command does not exist, and is not shown it.
func allowed(src cmd.Source) bool {
	p, ok := src.(*player.Player)
	if !ok {
		return false
	}
	m, ok := member(p.H())
	return ok && m.Operator
}

func (g typed) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	text := g.Item
	if n, ok := g.Amount.Load(); ok {
		text += " " + strconv.Itoa(n)
	}
	run(src, o, tx, g.Targets, text)
}

func (g raw) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	run(src, o, tx, g.Targets, string(g.Item))
}

// run gives the item to each target. The item text is taken from the line as
// typed when Remember kept it; Dragonfly's own split is the fallback.
func run(src cmd.Source, o *cmd.Output, tx *world.Tx, targets []cmd.Target, text string) {
	p, ok := src.(*player.Player)
	if !ok || tx == nil {
		o.Error("Only a player can use /give")
		return
	}
	if line, ok := recall(p.H()); ok {
		text = afterTarget(line)
	}
	req, err := ParseRequest(text)
	if err != nil {
		o.Error(explain(err, text))
		return
	}
	b, err := Build(req)
	if err != nil {
		o.Error(err)
		return
	}
	if limit := b.Stack.MaxCount() * 100; req.Count > limit {
		o.Errorf("Can't give more than %d of [%s]", limit, b.Name)
		return
	}
	var given []*player.Player
	for _, t := range targets {
		if to, ok := t.(*player.Player); ok {
			giveTo(tx, to, b.Stack, req.Count)
			given = append(given, to)
		}
	}
	if len(given) == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	r := Reply{Count: req.Count, Item: b, Targets: len(given)}
	if len(given) == 1 {
		r.Target = given[0].Name()
	}
	o.PrintMessage(r)
}

// afterTarget is a command line without its first argument, the target: a
// quoted name ("Some Player", as Bedrock writes names with spaces) or a word.
func afterTarget(line string) string {
	line = strings.TrimLeft(line, " ")
	if strings.HasPrefix(line, `"`) {
		if end := strings.IndexByte(line[1:], '"'); end >= 0 {
			return strings.TrimLeft(line[end+2:], " ")
		}
		return ""
	}
	if i := strings.IndexByte(line, ' '); i >= 0 {
		return strings.TrimLeft(line[i:], " ")
	}
	return ""
}

// explain is a syntax error as Java shows one: the message, then the text up
// to where it went wrong.
func explain(err error, text string) error {
	se, ok := err.(*SyntaxError)
	if !ok {
		return err
	}
	at := min(max(se.Pos, 0), len(text))
	from := max(0, at-10)
	prefix := ""
	if from > 0 {
		prefix = "..."
	}
	return fmt.Errorf("%s at position %d: %s%s<--[HERE]", se.Msg, at, prefix, text[from:at])
}

// pickupDelay is how long a dropped remainder waits before it can be picked up,
// by the player's edition (minecraft.wiki "Commands/give", oldid 3855978): none
// on Java, where the item is also kept for its owner (not done here: Dragonfly
// items have no owner), and 10 ticks on Bedrock.
func pickupDelay(p *player.Player) time.Duration {
	if m, ok := member(p.H()); ok && m.Java {
		return 0
	}
	return 500 * time.Millisecond
}

// giveTo puts count of s in the player's inventory, a stack at a time, and
// drops what does not fit at its feet, as vanilla does.
func giveTo(tx *world.Tx, p *player.Player, s item.Stack, count int) {
	per := s.MaxCount()
	for left := count; left > 0; {
		n := min(left, per)
		stack := s.Grow(n - s.Count())
		added, _ := p.Inventory().AddItem(stack)
		if rest := n - added; rest > 0 {
			opts := world.EntitySpawnOpts{Position: p.Position()}
			tx.AddEntity(entity.NewItemPickupDelay(opts, stack.Grow(rest-n), pickupDelay(p)))
		}
		left -= n
	}
}

// Reply is the success message: "Gave 1 [Diamond Sword] to Steve", or "to 3
// players". Java players get it as the game's own translation, with the item
// named in their language.
type Reply struct {
	Count   int
	Item    Built
	Target  string // the one player given to, "" for several
	Targets int
}

// String is the reply in English, as Bedrock players get it.
func (r Reply) String() string {
	if r.Target != "" {
		return fmt.Sprintf("Gave %d [%s] to %s", r.Count, r.Item.Name, r.Target)
	}
	return fmt.Sprintf("Gave %d [%s] to %d players", r.Count, r.Item.Name, r.Targets)
}

// JavaText is the reply as Java's commands.give.success.single or .multiple.
func (r Reply) JavaText() javaproto.Text {
	name := javaproto.Text{Text: r.Item.Stack.CustomName()}
	if name.Text == "" {
		switch {
		case r.Item.JavaName == "":
			name = javaproto.Text{Text: r.Item.Name}
		case r.Item.Block:
			name = javaproto.Text{Translate: "block.minecraft." + r.Item.JavaName}
		default:
			name = javaproto.Text{Translate: "item.minecraft." + r.Item.JavaName}
		}
	}
	item := javaproto.Text{Translate: "chat.square_brackets", With: []javaproto.Text{name}}
	count := javaproto.Text{Text: strconv.Itoa(r.Count)}
	if r.Target != "" {
		return javaproto.Text{Translate: "commands.give.success.single", With: []javaproto.Text{count, item, {Text: r.Target}}}
	}
	return javaproto.Text{Translate: "commands.give.success.multiple", With: []javaproto.Text{count, item, {Text: strconv.Itoa(r.Targets)}}}
}
