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
	out := flag.String("o", "map.png", "output PNG path")
	flag.Parse()

	img := draw(worldgen.New(*seed), *size, *scale)
	f, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
}

// draw colours each pixel from the height at its column, centred on the origin.
func draw(g *worldgen.Generator, size, scale int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	rows := make(chan int)
	var workers sync.WaitGroup
	for range runtime.NumCPU() {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for py := range rows {
				for px := range size {
					img.SetRGBA(px, py, heightColour(g.SurfaceHeight((px-size/2)*scale, (py-size/2)*scale)))
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
