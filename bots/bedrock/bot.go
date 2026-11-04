// Package bedrock is a small Bedrock test bot: it joins a server, walks and chats.
package bedrock

import (
	"context"
	"errors"
	"math"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

const (
	tick      = 50 * time.Millisecond
	walkSpeed = 0.2158 // blocks per tick, normal walking speed
)

// Bot is a connected Bedrock client.
type Bot struct {
	conn *minecraft.Conn
	name string

	mu       sync.Mutex
	pos      mgl32.Vec3
	angle    float64
	walking  bool
	tickNum  uint64
	chunks   atomic.Int64
	subs     atomic.Int64 // sub-chunk answers
	done     chan struct{}
	closeOne sync.Once

	yaw     float32
	ended   string                             // why the connection ended, if it has
	actions []protocol.PlayerBlockAction       // sent with the next movement packet
	others  map[uint64]*Seen                   // other players, by runtime ID
	blocks  map[protocol.BlockPos]uint32       // blocks changed since joining, layer 0
	texts   []Text                             // chat and server messages, in order
	listed  map[uuid.UUID]string               // the player list, by UUID
	skins   map[string]SkinSeen                // skins from the player list, by name
	windows map[uint32][]protocol.ItemInstance // window contents, by window ID
	openWin byte                               // the container window open, 0 if none
	opened  int                                // containers opened so far
	items   map[uint64]Item                    // item entities, by runtime ID
	taken   []Take                             // every pickup shown
	target  *mgl32.Vec2                        // a point (X, Z) to walk to
	sneak   bool                               // holding sneak
	once    []int                              // input flags for the next tick only
	meta    map[uint64]protocol.EntityMetadata // other entities' metadata, by runtime ID
	events  map[uint64][]int32                 // ActorEvent types, by runtime ID
	swings  map[uint64]int                     // arm swings shown, by runtime ID
	health  float32                            // our own, last sent
	motion  []mgl32.Vec3                       // SetActorMotion for ourselves
	rules   map[string]any                     // game rules from GameRulesChanged, by name
	cmds    []string                           // command names in the last AvailableCommands
	outputs []CommandOutput                    // every CommandOutput, in order
	reqs    uint32                             // command requests sent
}

// CommandOutput is one message of a command's output: a translation key with
// its parameters, or plain text.
type CommandOutput struct {
	Success bool
	Message string
	Params  []string
}

// Slot is an inventory slot as the bot was shown it.
type Slot struct {
	Name  string
	Meta  int16
	Count int
	NBT   map[string]any
}

// Item is an item entity the bot was shown.
type Item struct {
	Name  string
	Count int
	Pos   mgl64.Vec3
}

// Take is one pickup the bot was shown: an item entity and who took it.
type Take struct {
	Item, Taker uint64
}

// Text is one chat or server message the bot was sent.
type Text struct {
	Translation bool     // a key for the client to translate, with Params
	Source      string   // the sender of a chat message
	Message     string   // the text, or the translation key
	Params      []string // translation arguments
}

// Seen is what the bot knows about another player.
type Seen struct {
	Name string
	Pos  mgl64.Vec3 // feet
	Yaw  float64
	// What it holds and wears, as item names; "" for nothing.
	MainHand, OffHand                   string
	Helmet, Chestplate, Leggings, Boots string
}

// itemName names the item of a network item, as Dragonfly registers it.
func itemName(it protocol.ItemInstance) string {
	if it.Stack.NetworkID == 0 {
		return ""
	}
	i, ok := world.ItemByRuntimeID(it.Stack.NetworkID, int16(it.Stack.MetadataValue))
	if !ok {
		return "unknown"
	}
	name, _ := i.EncodeItem()
	return name
}

// Join connects to addr without Xbox login (the server must allow that) and
// waits until the bot has spawned. Each call is a new account.
func Join(addr, name string) (*Bot, error) {
	return JoinAs(addr, name, uuid.New())
}

// JoinAs is Join for a given account, so that the server knows it again.
func JoinAs(addr, name string, account uuid.UUID) (*Bot, error) {
	return JoinWith(addr, name, account, login.ClientData{})
}

// JoinWith is JoinAs with the client's own data, such as its skin. What it
// leaves empty gophertunnel fills in (its classic skin among it).
func JoinWith(addr, name string, account uuid.UUID, cd login.ClientData) (*Bot, error) {
	return join(name, account, cd, func(ctx context.Context, d minecraft.Dialer) (*minecraft.Conn, error) {
		return d.DialContext(ctx, "raknet", addr)
	})
}

// JoinNetherNet is Join over NetherNet, the way Bedrock 26.50 and later clients join: an HTTP
// handshake with the server at addr (host:port, on TCP), then WebRTC over UDP. The bot sends
// no Xbox identity, so the server must have login off.
func JoinNetherNet(addr, name string) (*Bot, error) {
	return join(name, uuid.New(), login.ClientData{}, func(ctx context.Context, d minecraft.Dialer) (*minecraft.Conn, error) {
		return d.DialContextNetwork(ctx, minecraft.NetherNet{}, "http://"+addr)
	})
}

func join(name string, account uuid.UUID, cd login.ClientData, dial func(context.Context, minecraft.Dialer) (*minecraft.Conn, error)) (*Bot, error) {
	d := minecraft.Dialer{IdentityData: login.IdentityData{DisplayName: name, Identity: account.String()}, ClientData: cd}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := dial(ctx, d)
	if err != nil {
		return nil, err
	}
	if err := conn.DoSpawnContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	b := &Bot{conn: conn, name: name, pos: conn.GameData().PlayerPosition, done: make(chan struct{}),
		others: map[uint64]*Seen{}, blocks: map[protocol.BlockPos]uint32{}, listed: map[uuid.UUID]string{}, skins: map[string]SkinSeen{}, windows: map[uint32][]protocol.ItemInstance{}, items: map[uint64]Item{},
		meta: map[uint64]protocol.EntityMetadata{}, events: map[uint64][]int32{}, swings: map[uint64]int{}, health: 20}
	go b.read()
	go b.loop()
	return b, nil
}

// SetViewDistance asks the server for a view distance in chunks, as a client's render distance
// setting does. The server may hold it lower.
func (b *Bot) SetViewDistance(chunks int) error {
	return b.conn.WritePacket(&packet.RequestChunkRadius{ChunkRadius: int32(chunks), MaxChunkRadius: uint8(chunks)})
}

// read drains packets so the connection never backs up.
func (b *Bot) read() {
	for {
		pk, err := b.conn.ReadPacket()
		if err != nil {
			b.mu.Lock()
			if b.ended == "" {
				b.ended = err.Error()
			}
			b.mu.Unlock()
			b.Close()
			return
		}
		if d, ok := pk.(*packet.Disconnect); ok {
			b.mu.Lock()
			b.ended = "disconnected: " + d.Message
			b.mu.Unlock()
		}
		switch pk.(type) {
		case *packet.SubChunk:
			b.chunks.Add(1)
			b.subs.Add(1)
		case *packet.LevelChunk:
			b.chunks.Add(1)
		default:
			b.watch(pk)
		}
	}
}

// watch keeps track of other players and changed blocks.
func (b *Bot) watch(pk packet.Packet) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch pk := pk.(type) {
	case *packet.MovePlayer:
		// The server moved the bot itself (a respawn, a teleport): a client goes on from there.
		if pk.EntityRuntimeID == b.conn.GameData().EntityRuntimeID && pk.Mode != packet.MoveModeNormal {
			b.pos = pk.Position
		}
	case *packet.GameRulesChanged:
		if b.rules == nil {
			b.rules = map[string]any{}
		}
		for _, r := range pk.GameRules {
			b.rules[r.Name] = r.Value
		}
	case *packet.AddPlayer:
		b.others[pk.EntityRuntimeID] = &Seen{Name: pk.Username, Pos: vec64(pk.Position), Yaw: float64(pk.Yaw)}
		b.meta[pk.EntityRuntimeID] = pk.EntityMetadata
	case *packet.SetActorData:
		b.meta[pk.EntityRuntimeID] = pk.EntityMetadata
	case *packet.ActorEvent:
		b.events[pk.EntityRuntimeID] = append(b.events[pk.EntityRuntimeID], int32(pk.EventType))
	case *packet.Animate:
		if pk.ActionType == packet.AnimateActionSwingArm {
			b.swings[pk.EntityRuntimeID]++
		}
	case *packet.UpdateAttributes:
		if pk.EntityRuntimeID == b.conn.GameData().EntityRuntimeID {
			for _, a := range pk.Attributes {
				if a.Name == "minecraft:health" {
					b.health = a.Value
				}
			}
		}
	case *packet.SetActorMotion:
		if pk.EntityRuntimeID == b.conn.GameData().EntityRuntimeID {
			b.motion = append(b.motion, pk.Velocity)
		}
	case *packet.MoveActorAbsolute:
		if s, ok := b.others[pk.EntityRuntimeID]; ok {
			s.Pos, s.Yaw = vec64(pk.Position).Sub(mgl64.Vec3{0, player.Type.NetworkOffset()}), float64(pk.Rotation[1])
		}
	case *packet.RemoveActor:
		delete(b.others, uint64(pk.EntityUniqueID))
		delete(b.items, uint64(pk.EntityUniqueID))
	case *packet.AddItemActor:
		b.items[pk.EntityRuntimeID] = Item{Name: itemName(pk.Item), Count: int(pk.Item.Stack.Count), Pos: vec64(pk.Position)}
	case *packet.TakeItemActor:
		b.taken = append(b.taken, Take{Item: pk.ItemEntityRuntimeID, Taker: pk.TakerEntityRuntimeID})
	case *packet.UpdateBlock:
		if pk.Layer == 0 {
			b.blocks[pk.Position] = pk.NewBlockRuntimeID
		}
	case *packet.MobEquipment:
		if s, ok := b.others[pk.EntityRuntimeID]; ok {
			if pk.WindowID == protocol.WindowIDOffHand {
				s.OffHand = itemName(pk.NewItem)
			} else {
				s.MainHand = itemName(pk.NewItem)
			}
		}
	case *packet.MobArmourEquipment:
		if s, ok := b.others[pk.EntityRuntimeID]; ok {
			s.Helmet, s.Chestplate, s.Leggings, s.Boots = itemName(pk.Helmet), itemName(pk.Chestplate), itemName(pk.Leggings), itemName(pk.Boots)
		}
	case *packet.AvailableCommands:
		b.cmds = b.cmds[:0]
		for _, c := range pk.Commands {
			b.cmds = append(b.cmds, c.Name)
		}
	case *packet.CommandOutput:
		for _, m := range pk.OutputMessages {
			b.outputs = append(b.outputs, CommandOutput{Success: m.Success, Message: m.Message, Params: m.Parameters})
		}
	case *packet.Text:
		b.texts = append(b.texts, Text{Translation: pk.TextType == packet.TextTypeTranslation,
			Source: pk.SourceName, Message: pk.Message, Params: pk.Parameters})
	case *packet.ContainerOpen:
		b.openWin = pk.WindowID
		b.opened++
	case *packet.ContainerClose:
		if pk.WindowID == b.openWin {
			b.openWin = 0
		}
	case *packet.InventoryContent:
		b.windows[pk.WindowID] = append([]protocol.ItemInstance(nil), pk.Content...)
	case *packet.InventorySlot:
		if w, ok := b.windows[pk.WindowID]; ok && int(pk.Slot) < len(w) {
			w[pk.Slot] = pk.NewItem
		}
	case *packet.PlayerList:
		for _, e := range pk.Entries {
			if e.ActionType == protocol.PlayerListActionAdd {
				b.listed[e.UUID] = e.Username
				s := skinSeen(e.Skin)
				s.Rows, s.RowPix = b.skins[e.Username].Rows+1, s.Pix
				b.skins[e.Username] = s
			} else {
				delete(b.listed, e.UUID)
			}
		}
	case *packet.PlayerSkin:
		if name, ok := b.listed[pk.UUID]; ok {
			s := skinSeen(pk.Skin)
			prev := b.skins[name]
			s.Changes, s.Rows, s.RowPix = prev.Changes+1, prev.Rows, prev.RowPix
			b.skins[name] = s
		}
	}
}

// Texts is every chat and server message so far.
func (b *Bot) Texts() []Text {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]Text(nil), b.texts...)
}

// Listed is the names in the bot's player list.
func (b *Bot) Listed() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for _, n := range b.listed {
		out = append(out, n)
	}
	return out
}

func vec64(v mgl32.Vec3) mgl64.Vec3 {
	return mgl64.Vec3{float64(v[0]), float64(v[1]), float64(v[2])}
}

// loop sends one movement packet per tick, like a real client.
func (b *Bot) loop() {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-b.done:
			return
		case <-t.C:
			b.mu.Lock()
			b.tickNum++
			pk := &packet.PlayerAuthInput{Tick: b.tickNum, Position: b.pos, Yaw: b.yaw, HeadYaw: b.yaw}
			if b.target != nil && !b.walking {
				d := mgl32.Vec2{b.target[0] - b.pos[0], b.target[1] - b.pos[2]}
				if d.Len() <= 0.3 {
					b.pos[0], b.pos[2], b.target = b.target[0], b.target[1], nil
				} else {
					d = d.Normalize().Mul(0.3)
					b.pos = b.pos.Add(mgl32.Vec3{d[0], 0, d[1]})
				}
				pk.Position = b.pos
			}
			if b.walking {
				dx, dz := float32(math.Cos(b.angle)*walkSpeed), float32(math.Sin(b.angle)*walkSpeed)
				b.pos = b.pos.Add(mgl32.Vec3{dx, 0, dz})
				pk.Position = b.pos
				pk.Delta = mgl32.Vec3{dx, 0, dz}
				b.yaw = float32(b.angle*180/math.Pi) - 90
				pk.Yaw, pk.HeadYaw = b.yaw, b.yaw
				pk.MoveVector = mgl32.Vec2{0, 1}
			}
			pk.InputData = protocol.NewInputFlags(packet.InputFlagCount)
			if len(b.actions) > 0 {
				pk.InputData.Set(packet.InputFlagPerformBlockActions)
				pk.BlockActions = protocol.Option(b.actions)
				b.actions = nil
			}
			if b.sneak {
				pk.InputData.Set(packet.InputFlagSneaking)
			}
			for _, f := range b.once {
				pk.InputData.Set(f)
			}
			b.once = nil
			b.mu.Unlock()
			if err := b.conn.WritePacket(pk); err != nil {
				return
			}
		}
	}
}

// Respawn asks to come back from the death screen, as the client's respawn button does.
func (b *Bot) Respawn() error {
	return b.conn.WritePacket(&packet.Respawn{State: packet.RespawnStateClientReadyToSpawn, EntityRuntimeID: b.conn.GameData().EntityRuntimeID})
}

// Walk starts walking in the direction angle (radians; 0 is +X, pi/2 is +Z).
func (b *Bot) Walk(angle float64) {
	b.mu.Lock()
	b.angle, b.walking = angle, true
	b.mu.Unlock()
}

// Jump moves the bot this far along +X in one packet, as a broken or hostile
// client might.
func (b *Bot) Jump(dx float32) {
	b.mu.Lock()
	b.pos[0] += dx
	b.mu.Unlock()
}

// GoTo walks the bot to (x, z), 0.3 blocks a tick.
func (b *Bot) GoTo(x, z float32) {
	b.mu.Lock()
	b.walking, b.target = false, &mgl32.Vec2{x, z}
	b.mu.Unlock()
}

// Sneak holds or lets go of sneak.
func (b *Bot) Sneak(on bool) {
	b.mu.Lock()
	b.sneak = on
	b.mu.Unlock()
}

// Sprint starts or stops sprinting.
func (b *Bot) Sprint(on bool) {
	b.mu.Lock()
	if on {
		b.once = append(b.once, packet.InputFlagStartSprinting)
	} else {
		b.once = append(b.once, packet.InputFlagStopSprinting)
	}
	b.mu.Unlock()
}

// Flag reports whether the bot was last told another player has a metadata
// flag (protocol.EntityDataFlag..., in the first flags key).
func (b *Bot) Flag(name string, flag uint8) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, s := range b.others {
		if s.Name == name {
			m, ok := b.meta[id]
			return ok && m.Flag(protocol.EntityDataKeyFlags, flag)
		}
	}
	return false
}

// Events is the ActorEvent types the bot was shown for another player, or for
// itself with its own name.
func (b *Bot) Events(name string) []int32 {
	b.mu.Lock()
	defer b.mu.Unlock()
	if name == b.name {
		return append([]int32(nil), b.events[b.conn.GameData().EntityRuntimeID]...)
	}
	for id, s := range b.others {
		if s.Name == name {
			return append([]int32(nil), b.events[id]...)
		}
	}
	return nil
}

// Swings is how many arm swings the bot was shown of another player.
func (b *Bot) Swings(name string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, s := range b.others {
		if s.Name == name {
			return b.swings[id]
		}
	}
	return 0
}

// Health is the bot's own health as the server last said.
func (b *Bot) Health() float32 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.health
}

// Pushes is every velocity the server gave the bot itself (knockback).
func (b *Bot) Pushes() []mgl32.Vec3 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]mgl32.Vec3(nil), b.motion...)
}

// Attack hits another player through an inventory transaction, as a client
// does, with what is in hotbar slot 0.
func (b *Bot) Attack(name string) error {
	id, ok := b.RuntimeIDOf(name)
	if !ok {
		return errors.New("bedrock bot: no such player in sight")
	}
	b.mu.Lock()
	pos := b.pos
	b.mu.Unlock()
	return b.conn.WritePacket(&packet.InventoryTransaction{TransactionData: &protocol.UseItemOnEntityTransactionData{
		TargetEntityRuntimeID: id,
		ActionType:            protocol.UseItemOnEntityActionAttack,
		HotBarSlot:            0,
		Position:              pos,
	}})
}

// Items is the item entities the bot sees, by runtime ID.
func (b *Bot) Items() map[uint64]Item {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[uint64]Item, len(b.items))
	for k, v := range b.items {
		out[k] = v
	}
	return out
}

// Taken is every pickup the bot was shown.
func (b *Bot) Taken() []Take {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]Take(nil), b.taken...)
}

// RuntimeID is the bot's own entity runtime ID.
func (b *Bot) RuntimeID() uint64 { return b.conn.GameData().EntityRuntimeID }

// RuntimeIDOf is the runtime ID of another player the bot sees.
func (b *Bot) RuntimeIDOf(name string) (uint64, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, s := range b.others {
		if s.Name == name {
			return id, true
		}
	}
	return 0, false
}

// Ended says why the connection ended, or "" while it is open.
func (b *Bot) Ended() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ended
}

// Stop stops walking.
func (b *Bot) Stop() {
	b.mu.Lock()
	b.walking = false
	b.mu.Unlock()
}

// Yaw is the direction the bot last faced, in degrees.
func (b *Bot) Yaw() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return float64(b.yaw)
}

// Player reports what the bot knows about the player with this name.
func (b *Bot) Player(name string) (Seen, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.others {
		if s.Name == name {
			return *s, true
		}
	}
	return Seen{}, false
}

// BlockIsAir reports whether the server has told the bot this block is now air.
func (b *Bot) BlockIsAir(pos cube.Pos) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	rid, ok := b.blocks[protocol.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}]
	return ok && rid == world.BlockRuntimeID(block.Air{})
}

// Block is the runtime ID the server last sent for the block at pos, layer 0,
// and whether it sent one since the bot joined.
func (b *Bot) Block(pos cube.Pos) (uint32, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	rid, ok := b.blocks[protocol.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}]
	return rid, ok
}

// Break starts breaking the block on its top face and finishes on a later tick,
// as a client does.
func (b *Bot) Break(pos cube.Pos) error {
	at := protocol.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	b.queue(protocol.PlayerBlockAction{Action: protocol.PlayerActionStartBreak, BlockPos: at, Face: int32(cube.FaceUp)})
	time.Sleep(3 * tick)
	b.queue(protocol.PlayerBlockAction{Action: protocol.PlayerActionPredictDestroyBlock, BlockPos: at, Face: int32(cube.FaceUp)})
	select {
	case <-b.done:
		return errors.New("bedrock bot: closed while breaking")
	default:
		return nil
	}
}

func (b *Bot) queue(a protocol.PlayerBlockAction) {
	b.mu.Lock()
	b.actions = append(b.actions, a)
	b.mu.Unlock()
}

// RequestSubChunk asks for one sub-chunk, as a real client does for each
// sub-chunk of a chunk sent in request mode: block entities come with it.
func (b *Bot) RequestSubChunk(x, y, z int32) error {
	return b.conn.WritePacket(&packet.SubChunkRequest{Position: protocol.SubChunkPos{x, y, z}, Offsets: []protocol.SubChunkOffset{{0, 0, 0}}})
}

// SubChunksReceived is how many sub-chunk answers the server has sent.
func (b *Bot) SubChunksReceived() int64 { return b.subs.Load() }

// ChunksReceived is how many chunk packets the server has sent so far.
func (b *Bot) ChunksReceived() int64 { return b.chunks.Load() }

// Command runs a command as a client does from its chat box, with its slash.
func (b *Bot) Command(line string) error {
	b.mu.Lock()
	b.reqs++
	n := b.reqs
	b.mu.Unlock()
	return b.conn.WritePacket(&packet.CommandRequest{
		CommandLine:   line,
		CommandOrigin: protocol.CommandOrigin{Origin: protocol.CommandOriginPlayer, UUID: uuid.New(), RequestID: strconv.Itoa(int(n))},
		Version:       protocol.CurrentVersion,
	})
}

// Commands is the command names the server last said the bot may run.
func (b *Bot) Commands() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.cmds...)
}

// CommandOutputs is every command output message so far.
func (b *Bot) CommandOutputs() []CommandOutput {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]CommandOutput(nil), b.outputs...)
}

// Inventory is the bot's 36 inventory slots as the server last sent them.
func (b *Bot) Inventory() []Slot {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []Slot
	for _, it := range b.windows[protocol.WindowIDInventory] {
		out = append(out, Slot{Name: itemName(it), Meta: int16(it.Stack.MetadataValue), Count: int(it.Stack.Count), NBT: it.Stack.NBTData})
	}
	return out
}

// Say sends a chat message.
func (b *Bot) Say(text string) {
	_ = b.conn.WritePacket(&packet.Text{
		TextType:   packet.TextTypeChat,
		SourceName: b.name,
		Message:    text,
	})
}

// Close leaves the server.
func (b *Bot) Close() error {
	var err error
	b.closeOne.Do(func() {
		close(b.done)
		err = b.conn.Close()
	})
	return err
}

// SkinSeen is a player's skin as the player list, or a later skin change,
// carried it.
type SkinSeen struct {
	Width, Height, Data int
	Patch               string // the resource patch: the geometry it uses
	Pix                 []byte // the image, RGBA
	Geometry            string // the geometry data
	Persona             bool
	Changes             int           // skin changes seen after the list's row
	Rows                int           // player list rows seen for the name
	RowPix              []byte        // the image the last list row carried, RGBA
	Raw                 protocol.Skin // the skin as the packet carried it, every field
}

func skinSeen(s protocol.Skin) SkinSeen {
	return SkinSeen{Width: int(s.SkinImageWidth), Height: int(s.SkinImageHeight), Data: len(s.SkinData), Patch: string(s.SkinResourcePatch),
		Pix: append([]byte(nil), s.SkinData...), Geometry: string(s.SkinGeometry), Persona: s.PersonaSkin, Raw: s}
}

// Skin is the skin the player list gave a player, by name, or the last skin
// change since.
func (b *Bot) Skin(name string) (SkinSeen, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.skins[name]
	return s, ok
}

// Container is the container window the bot has open: its ID, the containers
// opened so far, and what its slots hold, by item name and count.
func (b *Bot) Container() (id byte, opened int, slots []Item) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, it := range b.windows[uint32(b.openWin)] {
		slots = append(slots, Item{Name: itemName(it), Count: int(it.Stack.Count)})
	}
	return b.openWin, b.opened, slots
}

// TakeFromContainer asks, as a client does with an item stack request, to take
// count items from a slot of the open chest-like container into an empty
// inventory slot.
func (b *Bot) TakeFromContainer(slot, count, to byte) error {
	b.mu.Lock()
	w := b.windows[uint32(b.openWin)]
	var id int32
	if int(slot) < len(w) {
		id = w[slot].StackNetworkID
	}
	b.mu.Unlock()
	var take protocol.TakeStackRequestAction
	take.Count = count
	take.Source = protocol.StackRequestSlotInfo{Container: protocol.FullContainerName{ContainerID: protocol.ContainerLevelEntity}, Slot: slot, StackNetworkID: id}
	take.Destination = protocol.StackRequestSlotInfo{Container: protocol.FullContainerName{ContainerID: protocol.ContainerCombinedHotBarAndInventory}, Slot: to}
	return b.conn.WritePacket(&packet.ItemStackRequest{Requests: []protocol.ItemStackRequest{{RequestID: -1, Actions: []protocol.StackRequestAction{&take}}}})
}

// ChangeSkin changes the bot's skin, as a client does from its dressing room.
func (b *Bot) ChangeSkin(s protocol.Skin) error {
	id, err := uuid.Parse(b.conn.IdentityData().Identity)
	if err != nil {
		return err
	}
	return b.conn.WritePacket(&packet.PlayerSkin{UUID: id, Skin: s, NewSkinName: "custom", OldSkinName: "custom"})
}

// GameRule is the last value the server sent for a game rule in
// GameRulesChanged, and whether it sent one.
func (b *Bot) GameRule(name string) (any, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	v, ok := b.rules[name]
	return v, ok
}
