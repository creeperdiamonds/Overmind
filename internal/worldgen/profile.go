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
	infestedBedrock, extraMountainCoal         bool

	// Our own tuning, not Bedrock data: ores leave tuff alone, fitted to Bedrock's measured ore counts
	// with the cause unknown.
	oresSkipTuff bool
	// Attempts per chunk, BEDROCK-NOTES section 5 row 9: glow lichen 104 to 157 in both (Bedrock's file
	// agrees with Java's), underwater magma 18 to 30 in Bedrock's file against Java's 44 to 52.
	lichenCount, magmaCount [2]int
	// climate is each biome's temperature: Bedrock's file values where they differ from Java's.
	climate *climateTable

	placements []*placement
}

var (
	// ProfileBedrock is the default: Java's algorithms with Bedrock's data where it differs.
	ProfileBedrock = newProfile(&Profile{Name: "bedrock", bedrockFloorTop: minY + 5, bedrockMeasured: true,
		ironSize: 10, diamondMediumSize: 4, dirtReplaces: "stone", gravelReplaces: "stone deepslate", rockReplaces: "stone",
		oresSkipTuff: true, infestedBedrock: true, extraMountainCoal: true, climate: &bedrockClimates,
		lichenCount: [2]int{104, 157}, magmaCount: [2]int{18, 30}})
	// ProfileOverjava uses Java's values throughout.
	ProfileOverjava = newProfile(&Profile{Name: "overjava", bedrockFloorTop: minY + 5, ironSize: 9, diamondMediumSize: 8, climate: &biomeClimates,
		lichenCount: [2]int{104, 157}, magmaCount: [2]int{44, 52}})
)

func newProfile(p *Profile) *Profile {
	p.placements = buildPlacements(p)
	return p
}

// ProfileByName gives a profile by its Name, or nil.
func ProfileByName(name string) *Profile {
	for _, p := range []*Profile{ProfileBedrock, ProfileOverjava} {
		if p.Name == name {
			return p
		}
	}
	return nil
}
