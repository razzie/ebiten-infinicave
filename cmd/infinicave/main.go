package main

import (
	"flag"
	"log"
	"math/rand"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	seed := flag.Int64("seed", rand.Int63(), "random seed (random by default)")
	output := flag.String("output", "", "save the bottom 2400 pixels as a PNG and exit")
	texture := flag.Float64("texture", 8, "surface texture strength (0 disables it, range 0-16)")
	study := flag.String("study", "", "isolated rock study: ledge or curl (no vines)")
	view := flag.String("view", "shaded", "terrain view: shaded, clay, height, normals, shadows")
	hover := flag.Bool("hover", true, "highlight foreground rocks and guide lines under the mouse")
	tolerance := flag.Float64("collision-tolerance", 0, "collision polygon simplification tolerance in scene units (one section is 1 by 1)")
	flag.Parse()
	config := infinicave.DefaultConfig()
	config.Seed, config.Texture = *seed, *texture
	var err error
	config.Study, err = infinicave.ParseStudy(*study)
	if err != nil {
		return err
	}
	config.View, err = infinicave.ParseView(*view)
	if err != nil {
		return err
	}
	config.CollisionTolerance = *tolerance
	scene, err := infinicave.NewScene(config)
	if err != nil {
		return err
	}
	defer scene.Close()
	g := &Game{scene: scene, seed: *seed, output: *output, hover: *hover && *output == ""}

	ebiten.SetWindowSize(renderWidth, 800)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetWindowTitle("ebiten-infinicave | Scroll / Up / Down | R: regenerate")
	return ebiten.RunGame(g)
}
