// Package worldrules holds the shared world mechanics that Bedrock and Java play differently. Things the
// world shares can run only one way: Bedrock's under the default profile, Java's under Overjava
// (spec, "Each edition plays by its own rules" and "Overjava"). Values cite minecraft.wiki by oldid.
package worldrules

import "github.com/df-mc/dragonfly/server/world"

// Rules is one edition's world mechanics.
type Rules struct {
	// Name is the world profile these rules belong to.
	Name string
	// RandomTickSpeed is how many blocks of each sub chunk get a random tick each game tick.
	// Java's random_tick_speed defaults to 3 and is that count; Bedrock's randomTickSpeed defaults to 1
	// ("Tick", oldid 3868751). Bedrock's random-ticked growth is a third of Java's: sugar cane grows
	// every 16 random ticks, on average every 18 minutes on Java and 54 on Bedrock ("Sugar Cane",
	// oldid 3810018), which is one block per sub chunk a tick.
	RandomTickSpeed int
	// Mechanics are the rest, which the Dragonfly fork's worlds apply themselves.
	Mechanics world.Mechanics
}

var (
	// Bedrock is the default profile's rules.
	Bedrock = Rules{Name: "bedrock", RandomTickSpeed: 1}
	// Java is the Overjava profile's rules.
	Java = Rules{Name: "overjava", RandomTickSpeed: 3, Mechanics: world.Mechanics{
		// Leaves stay up to 6 steps from a log, against Bedrock's 4 ("Leaves", oldid 3873085), and any change
		// beside a leaf has it checked; Bedrock checks leaves only around a removed log or leaf (measured on
		// a Bedrock Dedicated Server, Task W report).
		JavaLeafDecay: true,
		// Snowfall lays one layer (max_snow_accumulation_height 1) against Bedrock's biome heights of 1 to
		// 12 ("Snow", oldid 3859371), and a waterlogged block beside water counts as water, so it does not
		// let it freeze; on Bedrock it does ("Ice", oldid 3877975).
		JavaWeather: true,
	}}
)

// ForProfile gives the rules of the world profile with this name: Java's for "overjava", Bedrock's
// for any other.
func ForProfile(name string) Rules {
	if name == Java.Name {
		return Java
	}
	return Bedrock
}
