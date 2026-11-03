package worldgen

// surfaceNoises holds the noises the surface rules read: the game's named noises, seeded as the game
// seeds them. The rules that read them stay our own design.
type surfaceNoises struct {
	depth, bands, gravel, calcite, ice, puddle, hoodoo, hoodooCap, berg, bergCap *normalNoise
	second, packed, powder, swamp, hoodooSurface, bergSurface                    *normalNoise
}

func newSurfaceNoises(w seedFactory) surfaceNoises {
	return surfaceNoises{
		depth:         newNamedNoise(w, "surface"),
		bands:         newNamedNoise(w, "clay_bands_offset"),
		gravel:        newNamedNoise(w, "gravel"),
		calcite:       newNamedNoise(w, "calcite"),
		ice:           newNamedNoise(w, "ice"),
		second:        newNamedNoise(w, "surface_secondary"),
		packed:        newNamedNoise(w, "packed_ice"),
		powder:        newNamedNoise(w, "powder_snow"),
		swamp:         newNamedNoise(w, "surface_swamp"),
		hoodooSurface: newNamedNoise(w, "badlands_surface"),
		bergSurface:   newNamedNoise(w, "iceberg_surface"),
		puddle:        newNamedNoise(w, "surface_swamp"),
		hoodoo:        newNamedNoise(w, "badlands_pillar"),
		hoodooCap:     newNamedNoise(w, "badlands_pillar_roof"),
		berg:          newNamedNoise(w, "iceberg_pillar"),
		bergCap:       newNamedNoise(w, "iceberg_pillar_roof"),
	}
}
