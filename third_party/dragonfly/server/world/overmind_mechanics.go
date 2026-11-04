package world

// Overmind fork: the shared world rules that Bedrock and Java play differently. Overmind's world follows
// Bedrock by default and Java under its Overjava profile, so the server sets these per world.

// RandomTickSpeed is how many blocks of each sub chunk get a random tick each tick, or less than 0 for
// none (Config.RandomTickSpeed, after its default).
func (w *World) RandomTickSpeed() int { return w.conf.RandomTickSpeed }
