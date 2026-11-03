package javaproto

// Combat packets. Layouts are minecraft-data's for 26.1.
var (
	PlayAttack       = id("play", "toServer", "attack")
	PlayUseEntity    = id("play", "toServer", "use_entity")
	PlayArmAnimation = id("play", "toServer", "arm_animation")

	PlayAnimation     = id("play", "toClient", "animation")
	PlayEntityStatus  = id("play", "toClient", "entity_status")
	PlayHurtAnimation = id("play", "toClient", "hurt_animation")
	PlayDamageEvent   = id("play", "toClient", "damage_event")
	PlayDeathCombat   = id("play", "toClient", "death_combat_event")
)

// ReadAttack decodes attack: the entity hit.
func ReadAttack(r *Reader) (int32, error) { id := r.VarInt(); return id, r.Done() }

// UseEntity is a decoded use_entity: the client used the item in a hand on an
// entity, at a point on it.
type UseEntity struct {
	Target, Hand int32
	X, Y, Z      float64
	Sneaking     bool
}

// ReadUseEntity decodes use_entity.
func ReadUseEntity(r *Reader) (UseEntity, error) {
	u := UseEntity{Target: r.VarInt(), Hand: r.VarInt()}
	u.X, u.Y, u.Z = r.ReadLpVec3()
	u.Sneaking = r.Bool()
	return u, r.Done()
}

// ReadArmAnimation decodes arm_animation: the hand swung.
func ReadArmAnimation(r *Reader) (int32, error) { h := r.VarInt(); return h, r.Done() }

// Animations of the animation packet. minecraft-data gives a u8; the values
// are minecraft.wiki's ("Entity Animation") for 26.1. They change in 26.3: the
// Packets page at oldid=3868170 (protocol 777) lists only 0 wake up, 1 critical
// and 2 magic critical, so a move to 26.3 must renumber these and swing arms
// another way.
const (
	AnimationSwingMainArm  = 0
	AnimationCritical      = 4
	AnimationMagicCritical = 5
)

// Animation plays an animation of an entity.
func Animation(entityID int32, animation byte) []byte {
	w := packet(PlayAnimation)
	w.VarInt(entityID)
	w.Byte(animation)
	return w.B
}

// Entity statuses. minecraft-data gives an i8; the values are minecraft.wiki's
// ("Entity statuses"): 3 plays a living entity's death sound.
const StatusDeath = 3

// EntityStatus sends an entity event. The entity ID is an int here, not a
// VarInt.
func EntityStatus(entityID int32, status int8) []byte {
	w := packet(PlayEntityStatus)
	w.Int(entityID)
	w.Byte(byte(status))
	return w.B
}

// HurtAnimation tilts the client's view as it is hurt from yaw (degrees, the
// direction the damage came from, relative to where it looks).
func HurtAnimation(entityID int32, yaw float32) []byte {
	w := packet(PlayHurtAnimation)
	w.VarInt(entityID)
	w.Float(yaw)
	return w.B
}

// DamageEvent shows an entity taking damage of a type (its place in the
// damage_type registry), caused by the entities cause and direct (entity ID
// plus one; 0 for none).
func DamageEvent(entityID, sourceType, cause, direct int32) []byte {
	w := packet(PlayDamageEvent)
	w.VarInt(entityID)
	w.VarInt(sourceType)
	w.VarInt(cause)
	w.VarInt(direct)
	w.Bool(false) // no source position
	return w.B
}

// DeathCombat tells the client its player died, with the message for its
// death screen.
func DeathCombat(playerID int32, msg Text) []byte {
	w := packet(PlayDeathCombat)
	w.VarInt(playerID)
	w.B = appendText(w.B, msg)
	return w.B
}
