package world

// SetLightningChance makes each ticked chunk strike with a 1 in n chance a tick during a storm, for
// tests (Overmind fork), and returns a function that restores the old chance.
func SetLightningChance(n int) (restore func()) {
	old := lightningChance
	lightningChance = n
	return func() { lightningChance = old }
}
