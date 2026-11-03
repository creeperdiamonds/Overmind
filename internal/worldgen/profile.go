package worldgen

// Profile holds what differs between Bedrock's and Java's placement rules. The algorithms are the
// same; only values differ. The default is Bedrock; Overjava is "Overmind as a Java server".
type Profile struct {
	Name string
	// bedrockFloorTop is the first Y with no bedrock; bedrockMeasured chooses Bedrock's measured
	// floor over Java's even fall.
	bedrockFloorTop int
	bedrockMeasured bool

	// Where the Bedrock data files differ from Java's ore table (BEDROCK-NOTES section 5).
	ironSize, diamondMediumSize                int
	dirtReplaces, gravelReplaces, rockReplaces string // Empty is any kind of stone.
	oresSkipTuff                               bool   // Tuned to Bedrock's counts, not a Bedrock rule.
	infestedBedrock, extraMountainCoal         bool

	placements []*placement
}

var (
	// ProfileBedrock is the default: Java's algorithms with Bedrock's data where it differs.
	ProfileBedrock = newProfile(&Profile{Name: "bedrock", bedrockFloorTop: minY + 5, bedrockMeasured: true,
		ironSize: 10, diamondMediumSize: 4, dirtReplaces: "stone", gravelReplaces: "stone deepslate", rockReplaces: "stone",
		oresSkipTuff: true, infestedBedrock: true, extraMountainCoal: true})
	// ProfileOverjava uses Java's values throughout.
	ProfileOverjava = newProfile(&Profile{Name: "overjava", bedrockFloorTop: minY + 5, ironSize: 9, diamondMediumSize: 8})
)

func newProfile(p *Profile) *Profile {
	p.placements = buildPlacements(p)
	return p
}
