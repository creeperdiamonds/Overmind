package javaproto

// Player state packets. Layouts are minecraft-data's for 26.1.
var (
	PlaySettings     = id("play", "toServer", "settings")
	PlayPlayerInput  = id("play", "toServer", "player_input")
	PlayEntityAction = id("play", "toServer", "entity_action")
)

// ClientSettings is the client's settings (minecraft-data's common_settings,
// sent in configuration and in play).
type ClientSettings struct {
	Locale        string
	ViewDistance  int8
	ChatFlags     int32
	ChatColors    bool
	SkinParts     byte // the skin layers shown, a bit each
	MainHand      int32
	TextFiltering bool
	ServerListing bool
	Particles     int32
}

// ReadClientSettings decodes the settings packet of either phase.
func ReadClientSettings(r *Reader) (ClientSettings, error) {
	s := ClientSettings{Locale: r.String(16), ViewDistance: int8(r.Byte()), ChatFlags: r.VarInt(), ChatColors: r.Bool()}
	s.SkinParts, s.MainHand = r.Byte(), r.VarInt()
	s.TextFiltering, s.ServerListing, s.Particles = r.Bool(), r.Bool(), r.VarInt()
	return s, r.Done()
}

// The bits of player_input, in minecraft-data's order (a u8 bitflags, the first
// flag lowest).
const (
	InputForward  = 1 << 0
	InputBackward = 1 << 1
	InputLeft     = 1 << 2
	InputRight    = 1 << 3
	InputJump     = 1 << 4
	InputShift    = 1 << 5
	InputSprint   = 1 << 6
)

// ReadPlayerInput decodes player_input: the keys held, as bits.
func ReadPlayerInput(r *Reader) (byte, error) { b := r.Byte(); return b, r.Done() }

// entity_action's actions, minecraft-data's mapper for it.
const (
	ActionStopSleeping    = 0
	ActionStartSprinting  = 1
	ActionStopSprinting   = 2
	ActionStartRidingJump = 3
	ActionStopRidingJump  = 4
	ActionOpenInventory   = 5
	ActionStartFallFlying = 6
)

// EntityAction is a decoded entity_action (Java's "player command").
type EntityAction struct {
	EntityID, Action, JumpBoost int32
}

// ReadEntityAction decodes entity_action.
func ReadEntityAction(r *Reader) (EntityAction, error) {
	a := EntityAction{EntityID: r.VarInt(), Action: r.VarInt(), JumpBoost: r.VarInt()}
	return a, r.Done()
}
