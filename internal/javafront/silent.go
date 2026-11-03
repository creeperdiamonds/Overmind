package javafront

import (
	"net"
	"time"

	"github.com/df-mc/dragonfly/server/entity/effect"
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
)

// silent is every Reporter and Viewer method the proof has nothing to do for.
// Each one moves to conn when its feature is built (chat, inventory, health and
// so on). The login.ClientData return is a plain struct in Player's signature;
// Java fills none of it. No Bedrock packet is involved.
type silent struct{ world.NopViewer }

func (silent) AddDebugShape(shape debug.Shape)                                                 {}
func (silent) ClearInputLocks()                                                                {}
func (silent) ClientData() login.ClientData                                                    { return login.ClientData{} }
func (silent) CloseDialogue()                                                                  {}
func (silent) CloseForm()                                                                      {}
func (silent) EnableCoordinates(enable bool)                                                   {}
func (silent) EnableInstantRespawn(enable bool)                                                {}
func (silent) HideHudElement(e hud.Element)                                                    {}
func (silent) HudElementHidden(e hud.Element) bool                                             { return false }
func (silent) InputLocked(l input.Lock) bool                                                   { return false }
func (silent) LockInput(l input.Lock)                                                          {}
func (silent) PlaySound(t world.Sound, pos mgl64.Vec3)                                         {}
func (silent) RemoveAllDebugShapes()                                                           {}
func (silent) RemoveBossBar()                                                                  {}
func (silent) RemoveDebugShape(shape debug.Shape)                                              {}
func (silent) RemoveScoreboard()                                                               {}
func (silent) RemoveViewLayer(entity world.Entity)                                             {}
func (silent) SendAbilities(c session.Controllable)                                            {}
func (silent) SendActionBarMessage(text string)                                                {}
func (silent) SendBossBar(text string, colour uint8, healthPercentage float64)                 {}
func (silent) SendChargeItemComplete()                                                         {}
func (silent) SendDebugShapes(dim world.Dimension)                                             {}
func (silent) SendDialogue(d dialogue.Dialogue, e world.Entity)                                {}
func (silent) SendEffect(e effect.Effect)                                                      {}
func (silent) SendEffectRemoval(e effect.Type)                                                 {}
func (silent) SendExperience(level int, progress float64)                                      {}
func (silent) SendForm(f form.Form)                                                            {}
func (silent) SendGameMode(c session.Controllable)                                             {}
func (silent) SendHudUpdates()                                                                 {}
func (silent) SendInputLocks()                                                                 {}
func (silent) SendJukeboxPopup(message string)                                                 {}
func (silent) SendPlayerSpawn(pos mgl64.Vec3)                                                  {}
func (silent) SendPopup(message string)                                                        {}
func (silent) SendScoreboard(sb *scoreboard.Scoreboard)                                        {}
func (silent) SendSpeed(speed float64)                                                         {}
func (silent) SendSubtitle(text string)                                                        {}
func (silent) SendTip(message string)                                                          {}
func (silent) SendTitle(text string)                                                           {}
func (silent) SendToast(title, message string)                                                 {}
func (silent) SetTitleDurations(fadeInDuration, remainDuration, fadeOutDuration time.Duration) {}
func (silent) ShowHudElement(e hud.Element)                                                    {}
func (silent) StartShowingEntity(e world.Entity)                                               {}
func (silent) StopShowingEntity(e world.Entity)                                                {}
func (silent) Transfer(ip net.IP, port int)                                                    {}
func (silent) UnlockInput(l input.Lock)                                                        {}
func (silent) ViewAlwaysShowNameTag(entity world.Entity, alwaysShow bool)                      {}
func (silent) ViewItemCooldown(item world.Item, duration time.Duration)                        {}
func (silent) ViewLayer() *world.ViewLayer                                                     { return nil }
func (silent) ViewNameTag(entity world.Entity, nameTag string)                                 {}
func (silent) ViewPublicAlwaysShowNameTag(entity world.Entity)                                 {}
func (silent) ViewPublicNameTag(entity world.Entity)                                           {}
func (silent) ViewPublicScoreTag(entity world.Entity)                                          {}
func (silent) ViewScoreTag(entity world.Entity, scoreTag string)                               {}
func (silent) ViewSleepingPlayers(sleeping, total int)                                         {}
func (silent) ViewVisibility(entity world.Entity, level world.VisibilityLevel)                 {}
func (silent) VisibleDebugShapes() []debug.Shape                                               { return nil }
