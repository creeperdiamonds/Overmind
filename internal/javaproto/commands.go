package javaproto

// PlayDeclareCommands is the command tree the client completes and checks
// commands with: declare_commands, minecraft-data's 26.1 layout.
var PlayDeclareCommands = id("play", "toClient", "declare_commands")

// Command node kinds and flags, the low bits of a node's flags byte: the kind
// in bits 0-1, then executable, has a redirect, has custom suggestions, and
// restricted (needs an operator to run when clicked in chat, 1.21.6+).
const (
	NodeRoot       = 0
	NodeLiteral    = 1
	NodeArgument   = 2
	NodeExecutable = 0x04
	NodeRestricted = 0x20
)

// Argument parsers by their place in the command_argument_type registry, as
// minecraft-data's 26.1 protocol.json lists them in command_node's parser
// mapper.
const (
	ParserInteger   = 3
	ParserEntity    = 6
	ParserItemStack = 14
)

// EntityPlayersOnly is minecraft:entity's flag for players only (0x01 would
// be a single entity).
const EntityPlayersOnly = 0x02

// CommandNode is one node of the tree. Props are the parser's properties,
// already encoded.
type CommandNode struct {
	Flags    byte
	Children []int32
	Name     string // a literal's word or an argument's name
	Parser   int32  // arguments only
	Props    []byte
}

// IntegerMin is brigadier:integer's properties with a minimum and no maximum.
func IntegerMin(min int32) []byte {
	w := &Writer{}
	w.Byte(0x01) // min present
	w.Int(min)
	return w.B
}

// DeclareCommands sends the tree: every node, then the root's index.
func DeclareCommands(nodes []CommandNode, root int32) []byte {
	w := packet(PlayDeclareCommands)
	w.VarInt(int32(len(nodes)))
	for _, n := range nodes {
		w.Byte(n.Flags)
		w.VarInt(int32(len(n.Children)))
		for _, c := range n.Children {
			w.VarInt(c)
		}
		switch n.Flags & 0x03 {
		case NodeLiteral:
			w.String(n.Name)
		case NodeArgument:
			w.String(n.Name)
			w.VarInt(n.Parser)
			w.B = append(w.B, n.Props...)
		}
	}
	w.VarInt(root)
	return w.B
}
