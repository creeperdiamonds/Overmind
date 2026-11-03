package javaproto

import (
	"encoding/binary"
	"math"

	"github.com/google/uuid"
)

// Play packet IDs, read from the generated tables. Names are minecraft-data's.
var (
	PlayLogin           = id("play", "toClient", "login")
	PlaySpawnPosition   = id("play", "toClient", "spawn_position")
	PlayGameEvent       = id("play", "toClient", "game_state_change")
	PlayCenterChunk     = id("play", "toClient", "update_view_position")
	PlayPlayerPosition  = id("play", "toClient", "position")
	PlayKeepAliveOut    = id("play", "toClient", "keep_alive")
	PlayDisconnect      = id("play", "toClient", "kick_disconnect")
	PlayBlockChange     = id("play", "toClient", "block_change")
	PlayAckDig          = id("play", "toClient", "acknowledge_player_digging")
	PlayUnloadChunk     = id("play", "toClient", "unload_chunk")
	PlayBatchStart      = id("play", "toClient", "chunk_batch_start")
	PlayBatchFinished   = id("play", "toClient", "chunk_batch_finished")
	PlayChunk           = id("play", "toClient", "map_chunk")
	PlayPlayerInfo      = id("play", "toClient", "player_info")
	PlayPlayerRemove    = id("play", "toClient", "player_remove")
	PlaySpawnEntity     = id("play", "toClient", "spawn_entity")
	PlayEntityMoveLook  = id("play", "toClient", "entity_move_look")
	PlayEntityHeadYaw   = id("play", "toClient", "entity_head_rotation")
	PlayEntitySync      = id("play", "toClient", "sync_entity_position")
	PlayEntitiesRemoved = id("play", "toClient", "entity_destroy")
	PlayHealth          = id("play", "toClient", "update_health")
	PlayRespawn         = id("play", "toClient", "respawn")
	PlaySectionChange   = id("play", "toClient", "multi_block_change")
	PlaySystemChat      = id("play", "toClient", "system_chat")
	PlayDisguisedChat   = id("play", "toClient", "profileless_chat")

	PlayTeleportConfirm = id("play", "toServer", "teleport_confirm")
	PlayKeepAliveIn     = id("play", "toServer", "keep_alive")
	PlayPosition        = id("play", "toServer", "position")
	PlayPositionLook    = id("play", "toServer", "position_look")
	PlayLook            = id("play", "toServer", "look")
	PlayDig             = id("play", "toServer", "block_dig")
	PlayBatchReceived   = id("play", "toServer", "chunk_batch_received")
	PlayClientCommand   = id("play", "toServer", "client_command")
)

// Int appends a big-endian int32.
func (w *Writer) Int(v int32) { w.B = binary.BigEndian.AppendUint32(w.B, uint32(v)) }

// Short appends a big-endian int16.
func (w *Writer) Short(v int16) { w.B = binary.BigEndian.AppendUint16(w.B, uint16(v)) }

// Byte appends one byte.
func (w *Writer) Byte(v byte) { w.B = append(w.B, v) }

// Float appends a big-endian float32.
func (w *Writer) Float(v float32) { w.B = binary.BigEndian.AppendUint32(w.B, math.Float32bits(v)) }

// Double appends a big-endian float64.
func (w *Writer) Double(v float64) { w.B = binary.BigEndian.AppendUint64(w.B, math.Float64bits(v)) }

// Position appends a block position packed as x:26, z:26, y:12.
func (w *Writer) Position(x, y, z int) {
	v := uint64(x&0x3FFFFFF)<<38 | uint64(z&0x3FFFFFF)<<12 | uint64(y&0xFFF)
	w.B = binary.BigEndian.AppendUint64(w.B, v)
}

// Double reads a big-endian float64.
func (r *Reader) Double() float64 {
	b := r.take(8)
	if b == nil {
		return 0
	}
	return math.Float64frombits(binary.BigEndian.Uint64(b))
}

// Float reads a big-endian float32.
func (r *Reader) Float() float32 {
	b := r.take(4)
	if b == nil {
		return 0
	}
	return math.Float32frombits(binary.BigEndian.Uint32(b))
}

// Int reads a big-endian int32.
func (r *Reader) Int() int32 {
	b := r.take(4)
	if b == nil {
		return 0
	}
	return int32(binary.BigEndian.Uint32(b))
}

// Byte reads one byte.
func (r *Reader) Byte() byte {
	b := r.take(1)
	if b == nil {
		return 0
	}
	return b[0]
}

// Position reads a block position packed as x:26, z:26, y:12.
func (r *Reader) Position() (x, y, z int) {
	v := r.Long()
	return int(v >> 38), int(v << 52 >> 52), int(v << 26 >> 38)
}

// Done fails the reader unless the whole packet was read, and returns Err.
func (r *Reader) Done() error {
	if r.err == nil && len(r.b) != 0 {
		r.fail()
	}
	return r.err
}

// Game events the front end sends.
const (
	EventStartWaitingForChunks = 13
)

// LoginInfo is what Login (play) tells the client about the game it joined.
type LoginInfo struct {
	EntityID         int32
	WorldNames       []string
	MaxPlayers       int32
	ViewDistance     int32
	DimensionType    int32
	DimensionName    string
	GameMode         byte
	PreviousGameMode int8
	SeaLevel         int32
}

// Login builds Login (play).
func Login(l LoginInfo) []byte {
	w := packet(PlayLogin)
	w.Int(l.EntityID)
	w.Bool(false) // hardcore
	w.VarInt(int32(len(l.WorldNames)))
	for _, n := range l.WorldNames {
		w.String(n)
	}
	w.VarInt(l.MaxPlayers)
	w.VarInt(l.ViewDistance)
	w.VarInt(l.ViewDistance) // simulation distance
	w.Bool(false)            // reduced debug info
	w.Bool(true)             // respawn screen
	w.Bool(false)            // limited crafting
	writeSpawnInfo(w, l)
	w.Bool(false) // secure chat
	return w.B
}

// writeSpawnInfo writes the part Login and Respawn share.
func writeSpawnInfo(w *Writer, l LoginInfo) {
	w.VarInt(l.DimensionType)
	w.String(l.DimensionName)
	w.Long(0) // hashed seed
	w.Byte(l.GameMode)
	w.Byte(byte(l.PreviousGameMode))
	w.Bool(false) // debug world
	w.Bool(false) // flat world
	w.Bool(false) // no death location
	w.VarInt(0)   // portal cooldown
	w.VarInt(l.SeaLevel)
}

// Respawn puts the client in a new level: back after its death screen, or in
// another dimension. keep is the data kept, a bit mask (minecraft.wiki Packets
// oldid=3868170, "Respawn": 0x01 attributes, 0x02 metadata).
func Respawn(l LoginInfo, keep byte) []byte {
	w := packet(PlayRespawn)
	writeSpawnInfo(w, l)
	w.Byte(keep)
	return w.B
}

// Health sets the health and food bars.
func Health(health float32, food int32, saturation float32) []byte {
	w := packet(PlayHealth)
	w.Float(health)
	w.VarInt(food)
	w.Float(saturation)
	return w.B
}

// SectionChange sets many blocks of one chunk section. Each record is a block
// state and the block's coordinates inside the section.
func SectionChange(sectionX, sectionY, sectionZ int32, records []int64) []byte {
	w := packet(PlaySectionChange)
	w.Long(int64(sectionX&0x3FFFFF)<<42 | int64(sectionZ&0x3FFFFF)<<20 | int64(sectionY&0xFFFFF))
	w.VarInt(int32(len(records)))
	for _, r := range records {
		w.B = appendVarLong(w.B, r)
	}
	return w.B
}

func appendVarLong(b []byte, v int64) []byte {
	u := uint64(v)
	for u >= 0x80 {
		b = append(b, byte(u)|0x80)
		u >>= 7
	}
	return append(b, byte(u))
}

// SpawnPosition builds the compass and respawn target.
func SpawnPosition(dimension string, x, y, z int) []byte {
	w := packet(PlaySpawnPosition)
	w.String(dimension)
	w.Position(x, y, z)
	w.Float(0)
	w.Float(0)
	return w.B
}

// GameEvent builds a Game Event.
func GameEvent(event byte, value float32) []byte {
	w := packet(PlayGameEvent)
	w.Byte(event)
	w.Float(value)
	return w.B
}

// CenterChunk tells the client which chunk its view is centred on.
func CenterChunk(x, z int32) []byte {
	w := packet(PlayCenterChunk)
	w.VarInt(x)
	w.VarInt(z)
	return w.B
}

// PlayerPosition moves the client's own player, all values absolute.
func PlayerPosition(teleportID int32, x, y, z float64, yaw, pitch float32) []byte {
	w := packet(PlayPlayerPosition)
	w.VarInt(teleportID)
	w.Double(x)
	w.Double(y)
	w.Double(z)
	w.Double(0)
	w.Double(0)
	w.Double(0)
	w.Float(yaw)
	w.Float(pitch)
	w.Int(0) // no relative fields
	return w.B
}

// KeepAlive asks the client to echo id.
func KeepAlive(id int64) []byte { w := packet(PlayKeepAliveOut); w.Long(id); return w.B }

// Disconnect ends play with a message.
func Disconnect(msg string) []byte {
	w := packet(PlayDisconnect)
	w.B = append(w.B, textNBT(msg)...)
	return w.B
}

// BlockChange sets one block.
func BlockChange(x, y, z int, state int32) []byte {
	w := packet(PlayBlockChange)
	w.Position(x, y, z)
	w.VarInt(state)
	return w.B
}

// AckDig acknowledges every dig up to sequence.
func AckDig(sequence int32) []byte { w := packet(PlayAckDig); w.VarInt(sequence); return w.B }

// UnloadChunk makes the client forget a chunk. Z comes first on the wire.
func UnloadChunk(x, z int32) []byte {
	w := packet(PlayUnloadChunk)
	w.Int(z)
	w.Int(x)
	return w.B
}

// BatchStart opens a batch of chunks.
func BatchStart() []byte { return packet(PlayBatchStart).B }

// BatchFinished closes a batch of n chunks.
func BatchFinished(n int32) []byte { w := packet(PlayBatchFinished); w.VarInt(n); return w.B }

// Player Info Update actions we use, as bits of its action byte.
const (
	infoAddPlayer  = 0x01
	infoGameMode   = 0x04
	infoListed     = 0x08
	infoLatency    = 0x10
	infoAllActions = infoAddPlayer | infoGameMode | infoListed | infoLatency
)

// PlayerInfoEntry is one row of the player list.
type PlayerInfoEntry struct {
	UUID              uuid.UUID
	Name              string
	Textures          string
	TexturesSignature string
	GameMode          int32
}

// PlayerInfoAdd adds one player to the client's player list. The client needs
// this before it can show that player's entity.
func PlayerInfoAdd(e PlayerInfoEntry) []byte {
	w := packet(PlayPlayerInfo)
	w.Byte(infoAllActions)
	w.VarInt(1)
	w.UUID(e.UUID)
	w.String(e.Name)
	if e.Textures == "" {
		w.VarInt(0)
	} else {
		w.VarInt(1)
		w.String("textures")
		w.String(e.Textures)
		w.Bool(e.TexturesSignature != "")
		if e.TexturesSignature != "" {
			w.String(e.TexturesSignature)
		}
	}
	w.VarInt(e.GameMode)
	w.VarInt(1) // listed
	w.VarInt(0) // latency
	return w.B
}

// PlayerRemove takes one player off the client's player list.
func PlayerRemove(u uuid.UUID) []byte {
	w := packet(PlayPlayerRemove)
	w.VarInt(1)
	w.UUID(u)
	return w.B
}

// Angle packs degrees into the protocol's 1/256 of a turn.
func Angle(degrees float64) byte {
	return byte(int64(math.Round(degrees*256/360)) & 0xFF)
}

// SpawnEntity shows an entity, moving at vel blocks per tick.
func SpawnEntity(entityID int32, u uuid.UUID, entityType int32, x, y, z, yaw, pitch float64, vel [3]float64) []byte {
	return SpawnEntityData(entityID, u, entityType, x, y, z, yaw, pitch, vel, 0)
}

// SpawnEntityData is SpawnEntity with its data field, whose meaning depends on
// the type: a falling block's block state, a projectile's owner
// (minecraft.wiki "Java Edition protocol/Object data" oldid=3328430).
func SpawnEntityData(entityID int32, u uuid.UUID, entityType int32, x, y, z, yaw, pitch float64, vel [3]float64, data int32) []byte {
	w := packet(PlaySpawnEntity)
	w.VarInt(entityID)
	w.UUID(u)
	w.VarInt(entityType)
	w.Double(x)
	w.Double(y)
	w.Double(z)
	w.B = AppendLpVec3(w.B, vel[0], vel[1], vel[2])
	w.Byte(Angle(pitch))
	w.Byte(Angle(yaw))
	w.Byte(Angle(yaw)) // head
	w.VarInt(data)
	return w.B
}

// EntityMoveLook moves an entity by deltas in 1/4096 of a block.
func EntityMoveLook(entityID int32, dx, dy, dz int16, yaw, pitch float64, onGround bool) []byte {
	w := packet(PlayEntityMoveLook)
	w.VarInt(entityID)
	w.Short(dx)
	w.Short(dy)
	w.Short(dz)
	w.Byte(Angle(yaw))
	w.Byte(Angle(pitch))
	w.Bool(onGround)
	return w.B
}

// EntityHeadYaw turns an entity's head.
func EntityHeadYaw(entityID int32, yaw float64) []byte {
	w := packet(PlayEntityHeadYaw)
	w.VarInt(entityID)
	w.Byte(Angle(yaw))
	return w.B
}

// EntitySync puts an entity at an absolute position, for moves too long for a
// delta and for teleports.
func EntitySync(entityID int32, x, y, z, yaw, pitch float64, onGround bool) []byte {
	w := packet(PlayEntitySync)
	w.VarInt(entityID)
	w.Double(x)
	w.Double(y)
	w.Double(z)
	w.Double(0)
	w.Double(0)
	w.Double(0)
	w.Float(float32(yaw))
	w.Float(float32(pitch))
	w.Bool(onGround)
	return w.B
}

// EntitiesRemoved hides entities.
func EntitiesRemoved(ids ...int32) []byte {
	w := packet(PlayEntitiesRemoved)
	w.VarInt(int32(len(ids)))
	for _, i := range ids {
		w.VarInt(i)
	}
	return w.B
}

// Movement is a decoded serverbound movement packet.
type Movement struct {
	X, Y, Z    float64
	Yaw, Pitch float32
	HasPos     bool
	HasLook    bool
}

// ReadMovement decodes a position, position_look or look packet body.
func ReadMovement(r *Reader, hasPos, hasLook bool) (Movement, error) {
	m := Movement{HasPos: hasPos, HasLook: hasLook}
	if hasPos {
		m.X, m.Y, m.Z = r.Double(), r.Double(), r.Double()
	}
	if hasLook {
		m.Yaw, m.Pitch = r.Float(), r.Float()
	}
	r.Byte() // flags; the server works out on-ground itself
	return m, r.Done()
}

// Client command actions.
const CommandRespawn = 0

// Dig is a decoded Player Action (block_dig).
type Dig struct {
	Status   int32
	X, Y, Z  int
	Face     byte
	Sequence int32
}

// ReadDig decodes a Player Action body.
func ReadDig(r *Reader) (Dig, error) {
	d := Dig{Status: r.VarInt()}
	d.X, d.Y, d.Z = r.Position()
	d.Face = r.Byte()
	d.Sequence = r.VarInt()
	return d, r.Done()
}
