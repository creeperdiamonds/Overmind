# worldgen

Our own vanilla-like Overworld generator. A chunk depends only on the seed and its position, so chunks can be built in any order on any number of workers.

- `noise.go`: seeded noise.
- `climate.go`: the five climate values at a column.
- `terrain.go`: turns climate into ground height, then 3D density for overhangs. `SurfaceHeight` lives here.
- `biomes.go`: picks a biome from climate (`BiomeAt`), and stores biomes in the chunk, 4x4x4 blocks at a time. Dripstone and lush caves sit 26 to 115 blocks under the ground.
- `surface.go`: covers the top of the ground with each biome's blocks (grass, sand, snow, red sand and terracotta bands, gravel, mycelium, ocean floors).
- `generator.go`: fills the chunk: stone, water up to Y 63, air above, then biomes, then surface.
- `blocks.go`: block IDs, looked up once.

The biome band edges (temperature, humidity, erosion, continentalness, valleys and peaks) come from the wiki's World generation page. Its tables of which biome sits in which cell are pictures we could not read, so which biome goes where is our own tuning, following the wiki's rules (snow when cold, desert when hot, and so on). Frozen oceans have no ice cap yet, so surface height stays equal to the top block.

The wiki gives climate band edges and how each value should affect terrain, and those are used as written. It does not publish spline heights or noise settings, so those numbers are our own tuning, and comments say which is which.

Worlds match across machines of the same CPU type. Go may round floating-point sums differently on ARM, so a seed could differ slightly there.
