package main

import (
	"flag"
	"log"
	"math"
	"math/rand"

	"github.com/hajimehoshi/ebiten/v2"
)

func main() {
	seed := flag.Int64("seed", rand.Int63(), "random seed (random by default)")
	output := flag.String("output", "", "save the bottom 2400 pixels as a PNG and exit")
	texture := flag.Float64("texture", 8, "surface texture strength (0 disables it, range 0-16)")
	study := flag.String("study", "", "isolated rock study: ledge or curl (no vines)")
	view := flag.String("view", "shaded", "terrain view: shaded, clay, height, normals, shadows")
	flag.Parse()
	if *study != "" && *study != "ledge" && *study != "curl" {
		log.Fatal("study must be ledge or curl")
	}
	switch *view {
	case "shaded", "clay", "height", "normals", "shadows":
	default:
		log.Fatal("unknown terrain view")
	}
	if *view != "shaded" {
		*texture = 0
	}
	if math.IsNaN(*texture) || *texture < 0 || *texture > 16 {
		log.Fatal("texture must be between 0 and 16")
	}
	material, err := ebiten.NewShader(materialShaderSource)
	if err != nil {
		log.Fatal(err)
	}
	defer material.Deallocate()
	vineMaterial, err := ebiten.NewShader(vineShaderSource)
	if err != nil {
		log.Fatal(err)
	}
	defer vineMaterial.Deallocate()

	g := &Game{seed: *seed, output: *output, material: material, vineMaterial: vineMaterial, texture: *texture, study: *study, view: *view}
	g.regenerate()
	defer func() { g.world.close() }()

	ebiten.SetWindowSize(W, 800)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetWindowTitle("Perlin + guide-warped Voronoi | Scroll / Up / Down | R: regenerate")
	if err := ebiten.RunGame(g); err != nil {
		log.Fatal(err)
	}
}
