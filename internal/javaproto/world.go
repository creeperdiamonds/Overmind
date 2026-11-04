package javaproto

// The world's state as a Java client sees it: its clock, its weather, sounds and level events. Layouts are
// minecraft-data's 26.1 protocol and minecraft.wiki "Java Edition protocol/Packets" (oldid 3868170).

var (
	PlayUpdateTime  = id("play", "toClient", "update_time")
	PlaySoundEffect = id("play", "toClient", "sound_effect")
	PlayWorldEvent  = id("play", "toClient", "world_event")
)

// Game events of the weather: "begin raining" and "end raining" set the rain level to 0 and 1, the
// opposite of their names, so each is followed by the level, as the vanilla server does.
const (
	EventBeginRaining       = 1
	EventEndRaining         = 2
	EventRainLevelChange    = 7
	EventThunderLevelChange = 8
)

// UpdateTime sets the world's age and one world clock: its time in ticks and how many clock ticks it
// advances each client tick (1 while the day cycles, 0 while it stands still). clock is the clock's place
// in the minecraft:world_clock registry.
func UpdateTime(age int64, clock int32, time int64, rate float32) []byte {
	w := packet(PlayUpdateTime)
	w.Long(age)
	w.VarInt(1)
	w.VarInt(clock)
	w.B = appendVarLong(w.B, time)
	w.Float(0)
	w.Float(rate)
	return w.B
}

// Sound categories (soundSource).
const (
	SoundWeather = 3
	SoundBlock   = 4
	SoundNeutral = 6
)

// SoundEffect plays a sound at a position, the sound given inline by name (ID 0, then the name, with no
// fixed range). The position is sent in eighths of a block.
func SoundEffect(name string, category int32, x, y, z float64, volume, pitch float32, seed int64) []byte {
	w := packet(PlaySoundEffect)
	w.VarInt(0)
	w.String(name)
	w.Bool(false)
	w.VarInt(category)
	w.Int(int32(x * 8))
	w.Int(int32(y * 8))
	w.Int(int32(z * 8))
	w.Float(volume)
	w.Float(pitch)
	w.Long(seed)
	return w.B
}

// World events the front end sends.
const (
	// WorldEventBlockBreak shows a block breaking, its particles and its sound, from its state ID.
	WorldEventBlockBreak = 2001
	// WorldEventEnderEyeShatter shows a thrown eye of ender breaking, with data 0.
	WorldEventEnderEyeShatter = 2003
)

// WorldEvent plays a level event at a block.
func WorldEvent(event int32, x, y, z int, data int32, global bool) []byte {
	w := packet(PlayWorldEvent)
	w.Int(event)
	w.Position(x, y, z)
	w.Int(data)
	w.Bool(global)
	return w.B
}
