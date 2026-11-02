// Command worldmap draws a seed's terrain as a PNG, seen from above.
package main

import (
	"flag"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"runtime"
	"sync"

	"github.com/overmind/overmind/internal/worldgen"
)

func main() {
	seed := flag.Int64("seed", 1, "world seed")
	size := flag.Int("size", 1024, "image width and height in pixels")
	scale := flag.Int("scale", 4, "blocks per pixel")
	byBiome := flag.Bool("biomes", false, "colour by biome instead of height")
	out := flag.String("o", "map.png", "output PNG path")
	flag.Parse()

	img := draw(worldgen.New(*seed), *size, *scale, *byBiome)
	f, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
}

// draw colours each pixel from the height at its column, or from its biome shaded by
// height, centred on the origin.
func draw(g *worldgen.Generator, size, scale int, byBiome bool) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	rows := make(chan int)
	var workers sync.WaitGroup
	for range runtime.NumCPU() {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for py := range rows {
				for px := range size {
					x, z := (px-size/2)*scale, (py-size/2)*scale
					h := g.SurfaceHeight(x, z)
					if byBiome {
						img.SetRGBA(px, py, biomeColour(g.BiomeAt(x, 319, z).String(), h))
					} else {
						img.SetRGBA(px, py, heightColour(h))
					}
				}
			}
		}()
	}
	for py := range size {
		rows <- py
	}
	close(rows)
	workers.Wait()
	return img
}

// heightColour maps water to blue by depth, and land through green, brown and white.
func heightColour(h int) color.RGBA {
	if h < worldgen.SeaLevel {
		return blend(color.RGBA{70, 130, 200, 255}, color.RGBA{10, 30, 90, 255}, float64(worldgen.SeaLevel-h)/40)
	}
	switch {
	case h < 80:
		return blend(color.RGBA{190, 200, 120, 255}, color.RGBA{70, 140, 60, 255}, float64(h-worldgen.SeaLevel)/17)
	case h < 130:
		return blend(color.RGBA{70, 140, 60, 255}, color.RGBA{120, 100, 70, 255}, float64(h-80)/50)
	default:
		return blend(color.RGBA{120, 100, 70, 255}, color.RGBA{250, 250, 250, 255}, float64(h-130)/110)
	}
}

func blend(a, b color.RGBA, t float64) color.RGBA {
	t = min(max(t, 0), 1)
	mix := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t) }
	return color.RGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), 255}
}

// biomeColours are rough map colours by Dragonfly's biome name. Anything missing is grey.
var biomeColours = map[string]color.RGBA{
	"plains": {141, 179, 96, 255}, "sunflower_plains": {170, 200, 80, 255}, "forest": {5, 102, 33, 255},
	"flower_forest": {45, 142, 73, 255}, "birch_forest": {48, 116, 68, 255}, "roofed_forest": {64, 81, 26, 255},
	"taiga": {11, 102, 89, 255}, "cold_taiga": {49, 85, 74, 255}, "ice_plains": {255, 255, 255, 255},
	"ice_plains_spikes": {180, 220, 220, 255}, "desert": {250, 148, 24, 255}, "savanna": {189, 178, 95, 255},
	"jungle": {83, 123, 9, 255}, "swampland": {7, 249, 178, 255}, "mesa": {217, 69, 21, 255},
	"meadow": {96, 165, 83, 255}, "grove": {120, 160, 150, 255}, "snowy_slopes": {200, 215, 230, 255},
	"jagged_peaks": {220, 220, 200, 255}, "frozen_peaks": {176, 176, 255, 255}, "stony_peaks": {130, 130, 120, 255},
	"extreme_hills": {96, 96, 96, 255}, "beach": {250, 222, 85, 255}, "cold_beach": {250, 240, 192, 255},
	"stone_beach": {162, 162, 132, 255}, "river": {0, 0, 255, 255}, "frozen_river": {160, 160, 255, 255},
	"mushroom_island": {255, 0, 255, 255}, "ocean": {0, 0, 112, 255}, "deep_ocean": {0, 0, 48, 255},
	"warm_ocean": {0, 0, 172, 255}, "deep_warm_ocean": {0, 0, 80, 255}, "lukewarm_ocean": {0, 0, 144, 255},
	"deep_lukewarm_ocean": {0, 0, 64, 255}, "cold_ocean": {32, 32, 112, 255}, "deep_cold_ocean": {32, 32, 56, 255},
	"frozen_ocean": {112, 112, 214, 255}, "deep_frozen_ocean": {64, 64, 144, 255},
}

// biomeColour shades a biome's colour a little by height so relief shows.
func biomeColour(name string, h int) color.RGBA {
	c, ok := biomeColours[name]
	if !ok {
		c = color.RGBA{128, 128, 128, 255}
	}
	shade := float64(min(max(h-worldgen.SeaLevel, -30), 120)) / 400
	if shade >= 0 {
		return blend(c, color.RGBA{255, 255, 255, 255}, shade)
	}
	return blend(c, color.RGBA{0, 0, 0, 255}, -shade)
}
