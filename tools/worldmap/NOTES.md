# worldmap

Draws a seed's terrain as a PNG, seen from above: blue for water, then green, brown and white as land rises.

`go run ./tools/worldmap -seed 1 -size 1024 -o map.png`

`-scale` sets blocks per pixel. The default of 4 makes a 1024 pixel map cover 4096 blocks, which is wide enough to see continents.

`-biomes` colours by biome instead of height, lightly shaded by height. Useful for checking where biomes land.
