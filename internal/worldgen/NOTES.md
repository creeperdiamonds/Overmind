# worldgen

Our own vanilla-like Overworld generator. A chunk depends only on the seed and its position, so chunks can be built in any order on any number of workers.

- `noise.go`: seeded noise.
- `climate.go`: the five climate values at a column.
- `terrain.go`: turns climate into ground height, then 3D density for overhangs. `SurfaceHeight` lives here.
- `generator.go`: fills the chunk: stone, water up to Y 63, air above.
- `blocks.go`: block IDs, looked up once.

The wiki gives climate band edges and how each value should affect terrain, and those are used as written. It does not publish spline heights or noise settings, so those numbers are our own tuning, and comments say which is which.

Worlds match across machines of the same CPU type. Go may round floating-point sums differently on ARM, so a seed could differ slightly there.
