package player

import (
	"net"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/player/debug"
	"github.com/df-mc/dragonfly/server/player/dialogue"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/player/hud"
	"github.com/df-mc/dragonfly/server/player/input"
	"github.com/df-mc/dragonfly/server/player/scoreboard"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"golang.org/x/text/language"
)

// Reporter is what a Player reports to: the client behind it, whichever
// edition that is. *session.Session (Bedrock) implements it unchanged; other
// front ends implement it directly. It lists exactly the methods Player calls.
// Overmind fork addition, see third_party/dragonfly/NOTES.md.
type Reporter interface {
	world.Viewer

	AddDebugShape(shape debug.Shape)
	Addr() net.Addr
	ChunkRadius() int32
	ClearInputLocks()
	ClientData() login.ClientData
	CloseContainer(tx *world.Tx)
	CloseDialogue()
	CloseForm()
	EnableCoordinates(enable bool)
	EnableInstantRespawn(enable bool)
	HideHudElement(e hud.Element)
	HudElementHidden(e hud.Element) bool
	InputLocked(l input.Lock) bool
	Latency() time.Duration
	LockInput(l input.Lock)
	OpenBlockContainer(pos cube.Pos, tx *world.Tx)
	OpenSign(pos cube.Pos, frontSide bool)
	PlaySound(t world.Sound, pos mgl64.Vec3)
	RemoveAllDebugShapes()
	RemoveBossBar()
	RemoveDebugShape(shape debug.Shape)
	RemoveScoreboard()
	RemoveViewLayer(entity world.Entity)
	SendAbilities(c session.Controllable)
	SendActionBarMessage(text string)
	SendBossBar(text string, colour uint8, healthPercentage float64)
	SendChargeItemComplete()
	SendCommandOutput(output *cmd.Output, l language.Tag)
	SendDebugShapes(dim world.Dimension)
	SendDialogue(d dialogue.Dialogue, e world.Entity)
	SendEffect(e effect.Effect)
	SendEffectRemoval(e effect.Type)
	SendExperience(level int, progress float64)
	SendFood(food int, saturation, exhaustion float64)
	SendForm(f form.Form)
	SendGameMode(c session.Controllable)
	SendHealth(health, max, absorption float64)
	SendHeldSlot(slot int, c session.Controllable, force bool)
	SendHudUpdates()
	SendInputLocks()
	SendJukeboxPopup(message string)
	SendMessage(message string)
	SendPlayerSpawn(pos mgl64.Vec3)
	SendPopup(message string)
	SendRespawn(pos mgl64.Vec3, c session.Controllable)
	SendScoreboard(sb *scoreboard.Scoreboard)
	SendSpeed(speed float64)
	SendSubtitle(text string)
	SendTip(message string)
	SendTitle(text string)
	SendToast(title, message string)
	SendTranslation(t chat.Translation, l language.Tag, a []any)
	SetTitleDurations(fadeInDuration, remainDuration, fadeOutDuration time.Duration)
	ShowHudElement(e hud.Element)
	StartShowingEntity(e world.Entity)
	StopShowingEntity(e world.Entity)
	Transfer(ip net.IP, port int)
	UnlockInput(l input.Lock)
	ViewAlwaysShowNameTag(entity world.Entity, alwaysShow bool)
	ViewItemCooldown(item world.Item, duration time.Duration)
	ViewLayer() *world.ViewLayer
	ViewNameTag(entity world.Entity, nameTag string)
	ViewPublicAlwaysShowNameTag(entity world.Entity)
	ViewPublicNameTag(entity world.Entity)
	ViewPublicScoreTag(entity world.Entity)
	ViewScoreTag(entity world.Entity, scoreTag string)
	ViewSleepingPlayers(sleeping, total int)
	ViewVisibility(entity world.Entity, level world.VisibilityLevel)
	VisibleDebugShapes() []debug.Shape
	Disconnect(message string)
	Close(tx *world.Tx, c session.Controllable)
	CloseConnection()
	HandleInventories(tx *world.Tx, c session.Controllable, inv, offHand, enderChest, ui *inventory.Inventory, armour *inventory.Armour, heldSlot *uint32)
}
