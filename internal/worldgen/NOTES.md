# worldgen

Our own vanilla-like Overworld generator. A chunk depends only on the seed and its position, so chunks can be built in any order on any number of workers.

- `noise.go`: seeded noise.
- `climate.go`: the five climate values at a column.
- `terrain.go`: turns climate into ground height, then 3D density for overhangs. `terrainHeight` is the top of the terrain before caves.
- `biomes.go`: picks a biome from climate (`BiomeAt`), and stores biomes in the chunk, 4x4x4 blocks at a time. Dripstone and lush caves sit 26 to 115 blocks under the ground.
- `surface.go`: covers the top of the ground with each biome's blocks (grass, sand, snow, red sand and terracotta bands, gravel, mycelium, ocean floors).
- `generator.go`: fills the chunk: stone (deepslate low down, bedrock at the bottom), water up to Y 63, air above, then biomes, surface, rock patches, caves and ores.
- `strata.go`: where stone turns to deepslate (Y 8 down to 0) and where bedrock thins out (Y -64 to -60).
- `caves.go`: cheese and spaghetti caves from 3D noise, lava below Y -54, simple water aquifers, and `SurfaceHeight`, the real top of the ground once caves are carved.
- `ores.go`: every ore and the granite, diorite, andesite and tuff patches, as blobs that each chunk rebuilds for itself and its eight neighbours, keeping only the blocks inside itself.
- `blocks.go`: block IDs, looked up once.

The biome band edges (temperature, humidity, erosion, continentalness, valleys and peaks) come from the wiki's World generation page. Its tables of which biome sits in which cell are pictures we could not read, so which biome goes where is our own tuning, following the wiki's rules (snow when cold, desert when hot, and so on). Frozen oceans have no ice cap yet, so surface height stays equal to the top block.

The wiki gives climate band edges and how each value should affect terrain, and those are used as written. It does not publish spline heights or noise settings, so those numbers are our own tuning, and comments say which is which.

Worlds match across machines of the same CPU type. Go may round floating-point sums differently on ARM, so a seed could differ slightly there.

Rivers follow the zero line of their own noise (1600-block wavelength, about 16 blocks wide), so they are single continuous courses several hundred blocks apart, about 1.5-3% of land. Terrain is pulled down to the valley floor along the same line (`terrainPV`). Peaks-and-valleys valleys from weirdness no longer make rivers. Beach width varies with weirdness. Climate noises are smoother than before (fewer octaves, longer weirdness and continentalness wavelengths), which makes biome regions larger. Surface materials were checked against minecraft.wiki biome pages; `surface.go` lists what the pages say and what is our own tuning (snowy plains and snowy taiga are grass without snow layers; plain ocean floors are mostly gravel; stony shore is stone with gravel strips; windswept hills are patchy grass and stone).

Underground: caves, ore ranges and sizes follow the wiki's Cave page and the individual ore pages; the cave noise settings, the blob shape and the rock patch sizes are our own tuning, and the comments in `caves.go` and `ores.go` say which numbers are which. Left out for now: noodle caves, carver caves and canyons, lava aquifers (lava stays below Y -54), and aquifer stone barriers (water that would hang in the air drains instead).

`SurfaceHeight` now counts caves: where a cave opens the top of the ground it returns the lower height, and it matches the generated chunk. `terrainHeight` is the height before caves, and is what biomes use. Under ocean and river water the floor is kept (4 blocks), and caves reach the sky only where an entrance noise is high (about 0.25% of land columns).

Veins and caves look only at the chunk being built. A vein's air-exposure rule only sees air inside the chunk, so a block touching air in the next chunk counts as not exposed.
