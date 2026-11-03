package worldgen

// Profile holds what differs between Bedrock's and Java's placement rules. The algorithms are the
// same; only values differ. The default is Bedrock; Overjava is "Overmind as a Java server".
type Profile struct {
	Name string
	// bedrockFloorTop is the first Y with no bedrock; bedrockMeasured chooses Bedrock's measured
	// floor over Java's even fall.
	bedrockFloorTop int
	bedrockMeasured bool
}

var (
	// ProfileBedrock is the default: Java's algorithms with Bedrock's data where it differs.
	ProfileBedrock = &Profile{Name: "bedrock", bedrockFloorTop: minY + 5, bedrockMeasured: true}
	// ProfileOverjava uses Java's values throughout.
	ProfileOverjava = &Profile{Name: "overjava", bedrockFloorTop: minY + 5}
)
