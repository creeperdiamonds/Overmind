package bridge

import (
	"net"
	"time"

	"github.com/overmind/bedrock-dev/server/block/cube"
	"github.com/overmind/bedrock-dev/server/cmd"
	"github.com/overmind/bedrock-dev/server/item"
	"github.com/overmind/bedrock-dev/server/player"
	"github.com/overmind/bedrock-dev/server/player/skin"
	"github.com/overmind/bedrock-dev/server/session"
	"github.com/overmind/bedrock-dev/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// PlayerHandler implements player.Handler.  It embeds NopHandler so that only
// the two bridge-relevant methods — HandleMove and HandleQuit — need a body.
type PlayerHandler struct {
	player.NopHandler
	b        *Bridge
	name     string
	onQuit   func()
}

// NewPlayerHandler returns a PlayerHandler that emits bridge events for the
// named player.  Call player.Player.Handle(h) immediately after receiving the
// player in the Accept() loop.
func NewPlayerHandler(b *Bridge, name string) *PlayerHandler {
	return &PlayerHandler{b: b, name: name}
}

// OnQuit registers a callback invoked when the player disconnects.
// Used by main.go to remove the player from the command-dispatch map.
func (h *PlayerHandler) OnQuit(fn func()) { h.onQuit = fn }

// HandleMove fires on every position change and forwards the new coordinates
// to all connected Java bridge clients.
func (h *PlayerHandler) HandleMove(_ *player.Context, newPos mgl64.Vec3, newRot cube.Rotation) {
	h.b.Emit(PlayerEvent{
		Type:  EventMove,
		Name:  h.name,
		X:     newPos[0],
		Y:     newPos[1],
		Z:     newPos[2],
		Yaw:   newRot.Yaw(),
		Pitch: newRot.Pitch(),
	})
}

// HandleQuit fires when the player disconnects for any reason.
func (h *PlayerHandler) HandleQuit(_ *player.Player) {
	h.b.Emit(PlayerEvent{Type: EventLeave, Name: h.name})
	if h.onQuit != nil {
		h.onQuit()
	}
}

// ── NopHandler pass-throughs required to satisfy the full player.Handler interface ──
// All other methods are already provided by the embedded NopHandler.

func (h *PlayerHandler) HandleJump(_ *player.Player)                                               {}
func (h *PlayerHandler) HandleTeleport(_ *player.Context, _ mgl64.Vec3)                           {}
func (h *PlayerHandler) HandleChangeWorld(_ *player.Player, _, _ *world.World)                    {}
func (h *PlayerHandler) HandleToggleSprint(_ *player.Context, _ bool)                             {}
func (h *PlayerHandler) HandleToggleSneak(_ *player.Context, _ bool)                              {}
func (h *PlayerHandler) HandleChat(_ *player.Context, _ *string)                                  {}
func (h *PlayerHandler) HandleFoodLoss(_ *player.Context, _ int, _ *int)                          {}
func (h *PlayerHandler) HandleHeal(_ *player.Context, _ *float64, _ world.HealingSource)          {}
func (h *PlayerHandler) HandleHurt(_ *player.Context, _ *float64, _ bool, _ *time.Duration, _ world.DamageSource) {
}
func (h *PlayerHandler) HandleDeath(_ *player.Player, _ world.DamageSource, _ *bool)              {}
func (h *PlayerHandler) HandleRespawn(_ *player.Player, _ *mgl64.Vec3, _ **world.World)           {}
func (h *PlayerHandler) HandleSkinChange(_ *player.Context, _ *skin.Skin)                         {}
func (h *PlayerHandler) HandleFireExtinguish(_ *player.Context, _ cube.Pos)                       {}
func (h *PlayerHandler) HandleStartBreak(_ *player.Context, _ cube.Pos)                           {}
func (h *PlayerHandler) HandleBlockBreak(_ *player.Context, _ cube.Pos, _ *[]item.Stack, _ *int)  {}
func (h *PlayerHandler) HandleBlockPlace(_ *player.Context, _ cube.Pos, _ world.Block)            {}
func (h *PlayerHandler) HandleBlockPick(_ *player.Context, _ cube.Pos, _ world.Block)             {}
func (h *PlayerHandler) HandleItemUse(_ *player.Context)                                          {}
func (h *PlayerHandler) HandleItemUseOnBlock(_ *player.Context, _ cube.Pos, _ cube.Face, _ mgl64.Vec3) {
}
func (h *PlayerHandler) HandleItemUseOnEntity(_ *player.Context, _ world.Entity)                  {}
func (h *PlayerHandler) HandleItemRelease(_ *player.Context, _ item.Stack, _ time.Duration)       {}
func (h *PlayerHandler) HandleItemConsume(_ *player.Context, _ item.Stack)                        {}
func (h *PlayerHandler) HandleAttackEntity(_ *player.Context, _ world.Entity, _, _ *float64, _ *bool) {
}
func (h *PlayerHandler) HandleExperienceGain(_ *player.Context, _ *int)                           {}
func (h *PlayerHandler) HandlePunchAir(_ *player.Context)                                         {}
func (h *PlayerHandler) HandleSignEdit(_ *player.Context, _ cube.Pos, _ bool, _, _ string)        {}
func (h *PlayerHandler) HandleSleep(_ *player.Context, _ *bool)                                   {}
func (h *PlayerHandler) HandleLecternPageTurn(_ *player.Context, _ cube.Pos, _ int, _ *int)       {}
func (h *PlayerHandler) HandleItemDamage(_ *player.Context, _ item.Stack, _ *int)                 {}
func (h *PlayerHandler) HandleItemPickup(_ *player.Context, _ *item.Stack)                        {}
func (h *PlayerHandler) HandleHeldSlotChange(_ *player.Context, _, _ int)                         {}
func (h *PlayerHandler) HandleItemDrop(_ *player.Context, _ item.Stack)                           {}
func (h *PlayerHandler) HandleTransfer(_ *player.Context, _ *net.UDPAddr)                         {}
func (h *PlayerHandler) HandleCommandExecution(_ *player.Context, _ cmd.Command, _ []string)      {}
func (h *PlayerHandler) HandleDiagnostics(_ *player.Player, _ session.Diagnostics)               {}
