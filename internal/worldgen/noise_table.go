package worldgen

// noiseSpecs is the game's named noises: first octave and amplitudes, from the study's list (Java 26.1,
// noise_params.csv), as our own table. A world's noises are seeded from their names, so creation order
// does not matter. Only noises this generator reads are built; the rest are here so the golden test can
// check every one of the 57 the study fixed.
var noiseSpecs = map[string]struct {
	first int
	amps  []float64
}{
	"aquifer_barrier":                 {-3, []float64{1.0}},
	"aquifer_fluid_level_floodedness": {-7, []float64{1.0}},
	"aquifer_fluid_level_spread":      {-5, []float64{1.0}},
	"aquifer_lava":                    {-1, []float64{1.0}},
	"badlands_pillar":                 {-2, []float64{1.0, 1.0, 1.0, 1.0}},
	"badlands_pillar_roof":            {-8, []float64{1.0}},
	"badlands_surface":                {-6, []float64{1.0, 1.0, 1.0}},
	"calcite":                         {-9, []float64{1.0, 1.0, 1.0, 1.0}},
	"cave_cheese":                     {-8, []float64{0.5, 1.0, 2.0, 1.0, 2.0, 1.0, 0.0, 2.0, 0.0}},
	"cave_entrance":                   {-7, []float64{0.4, 0.5, 1.0}},
	"cave_layer":                      {-8, []float64{1.0}},
	"clay_bands_offset":               {-8, []float64{1.0}},
	"continentalness":                 {-9, []float64{1.0, 1.0, 2.0, 2.0, 2.0, 1.0, 1.0, 1.0, 1.0}},
	"continentalness_large":           {-11, []float64{1.0, 1.0, 2.0, 2.0, 2.0, 1.0, 1.0, 1.0, 1.0}},
	"erosion":                         {-9, []float64{1.0, 1.0, 0.0, 1.0, 1.0}},
	"erosion_large":                   {-11, []float64{1.0, 1.0, 0.0, 1.0, 1.0}},
	"gravel":                          {-8, []float64{1.0, 1.0, 1.0, 1.0}},
	"gravel_layer":                    {-8, []float64{1.0, 1.0, 1.0, 1.0, 0.0, 0.0, 0.0, 0.0, 0.013333333333333334}},
	"ice":                             {-4, []float64{1.0, 1.0, 1.0, 1.0}},
	"iceberg_pillar":                  {-6, []float64{1.0, 1.0, 1.0, 1.0}},
	"iceberg_pillar_roof":             {-3, []float64{1.0}},
	"iceberg_surface":                 {-6, []float64{1.0, 1.0, 1.0}},
	"jagged":                          {-16, []float64{1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0}},
	"noodle":                          {-8, []float64{1.0}},
	"noodle_ridge_a":                  {-7, []float64{1.0}},
	"noodle_ridge_b":                  {-7, []float64{1.0}},
	"noodle_thickness":                {-8, []float64{1.0}},
	"offset":                          {-3, []float64{1.0, 1.0, 1.0, 0.0}},
	"ore_gap":                         {-5, []float64{1.0}},
	"ore_vein_a":                      {-7, []float64{1.0}},
	"ore_vein_b":                      {-7, []float64{1.0}},
	"ore_veininess":                   {-8, []float64{1.0}},
	"packed_ice":                      {-7, []float64{1.0, 1.0, 1.0, 1.0}},
	"patch":                           {-5, []float64{1.0, 0.0, 0.0, 0.0, 0.0, 0.013333333333333334}},
	"pillar":                          {-7, []float64{1.0, 1.0}},
	"pillar_rareness":                 {-8, []float64{1.0}},
	"pillar_thickness":                {-8, []float64{1.0}},
	"powder_snow":                     {-6, []float64{1.0, 1.0, 1.0, 1.0}},
	"ridge":                           {-7, []float64{1.0, 2.0, 1.0, 0.0, 0.0, 0.0}},
	"soul_sand_layer":                 {-8, []float64{1.0, 1.0, 1.0, 1.0, 0.0, 0.0, 0.0, 0.0, 0.013333333333333334}},
	"spaghetti_2d":                    {-7, []float64{1.0}},
	"spaghetti_2d_elevation":          {-8, []float64{1.0}},
	"spaghetti_2d_modulator":          {-11, []float64{1.0}},
	"spaghetti_2d_thickness":          {-11, []float64{1.0}},
	"spaghetti_3d_1":                  {-7, []float64{1.0}},
	"spaghetti_3d_2":                  {-7, []float64{1.0}},
	"spaghetti_3d_rarity":             {-11, []float64{1.0}},
	"spaghetti_3d_thickness":          {-8, []float64{1.0}},
	"spaghetti_roughness":             {-5, []float64{1.0}},
	"spaghetti_roughness_modulator":   {-8, []float64{1.0}},
	"surface":                         {-6, []float64{1.0, 1.0, 1.0}},
	"surface_secondary":               {-6, []float64{1.0, 1.0, 0.0, 1.0}},
	"surface_swamp":                   {-2, []float64{1.0}},
	"temperature":                     {-10, []float64{1.5, 0.0, 1.0, 0.0, 0.0, 0.0}},
	"temperature_large":               {-12, []float64{1.5, 0.0, 1.0, 0.0, 0.0, 0.0}},
	"vegetation":                      {-8, []float64{1.0, 1.0, 0.0, 0.0, 0.0, 0.0}},
	"vegetation_large":                {-10, []float64{1.0, 1.0, 0.0, 0.0, 0.0, 0.0}},
}

// newNamedNoise builds a named noise of a world.
func newNamedNoise(w seedFactory, name string) *normalNoise {
	spec, ok := noiseSpecs[name]
	if !ok {
		panic("worldgen: unknown noise " + name)
	}
	r := w.named("minecraft:" + name)
	return newNormalNoise(&r, spec.first, spec.amps)
}
