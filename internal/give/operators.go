package give

import (
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// Operators is the Operators setting: players who may use /give, by name (on
// either edition, any case), Java account UUID or Bedrock XUID.
type Operators struct {
	names map[string]bool
	ids   map[uuid.UUID]bool
	xuids map[string]bool
}

// NewOperators reads the setting's entries. An entry that parses as a UUID is
// a Java account; any other is a name, and one of digits only an XUID too.
func NewOperators(entries []string) Operators {
	o := Operators{names: map[string]bool{}, ids: map[uuid.UUID]bool{}, xuids: map[string]bool{}}
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if id, err := uuid.Parse(e); err == nil {
			o.ids[id] = true
			continue
		}
		if strings.Trim(e, "0123456789") == "" {
			o.xuids[e] = true // and a name: a Java name may be digits only
		}
		o.names[strings.ToLower(e)] = true
	}
	return o
}

// Includes reports whether a player is an operator. A Java player is matched by
// name or account UUID, a Bedrock player by name or XUID.
func (o Operators) Includes(name string, java bool, id uuid.UUID, xuid string) bool {
	if o.names[strings.ToLower(name)] {
		return true
	}
	if java {
		return o.ids[id]
	}
	return xuid != "" && o.xuids[xuid]
}

// Member is what /give knows of a player online: whether it may use the
// command, and its edition, whose rules its dropped items follow.
type Member struct {
	Operator bool
	Java     bool
}

// members are the players online on every server in the process, by entity
// handle. cmd.Register is process-wide, so the command finds its server's
// verdict on a player here rather than in a setting of its own.
var members sync.Map // *world.EntityHandle -> *memberState

type memberState struct {
	Member
	mu   sync.Mutex
	line string // the last command line the player ran, as typed
}

// Join records a player that has joined, before it can run a command.
func Join(h *world.EntityHandle, m Member) { members.Store(h, &memberState{Member: m}) }

// Leave forgets a player that has left.
func Leave(h *world.EntityHandle) { members.Delete(h) }

func member(h *world.EntityHandle) (*memberState, bool) {
	v, ok := members.Load(h)
	if !ok {
		return nil, false
	}
	return v.(*memberState), true
}

// Remember keeps the command line a player is about to run, exactly as typed.
// Dragonfly splits arguments with a CSV reader that treats a word starting with
// a quote as quoted, which would change a component value such as
// lore=["one", "two"]; /give reads its item from this line instead. Call it
// from the player's HandleCommandExecution, which runs on the same goroutine
// just before the command does.
func Remember(h *world.EntityHandle, args []string) {
	if m, ok := member(h); ok {
		m.mu.Lock()
		m.line = strings.Join(args, " ")
		m.mu.Unlock()
	}
}

// recall takes the line Remember kept, once.
func recall(h *world.EntityHandle) (string, bool) {
	m, ok := member(h)
	if !ok {
		return "", false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	l, had := m.line, m.line != ""
	m.line = ""
	return l, had
}
