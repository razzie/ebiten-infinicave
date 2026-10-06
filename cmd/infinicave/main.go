package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"

	"github.com/hajimehoshi/ebiten/v2"
	infinicave "github.com/razzie/ebiten-infinicave"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	defaults := infinicave.DefaultConfig()
	seed := flag.Int64("seed", rand.Int63(), "random seed (random by default)")
	output := flag.String("output", "", "save the bottom 2400 pixels as a PNG and exit")
	texture := flag.Float64("texture", 8, "surface texture strength (0 disables it, range 0-16)")
	backgroundBlur := flag.Float64("background-blur", defaults.BackgroundBlur, "background rock and vine blur in scene units (0 disables it, range 0-0.05)")
	shadowOpacity := flag.Float64("shadow-opacity", defaults.ShadowOpacity, "dynamic rock shadow opacity (0 disables it, range 0-1)")
	shadowBlur := flag.Float64("shadow-blur", defaults.ShadowBlur, "dynamic rock shadow softness in scene units (range 0-0.05)")
	shadowX := flag.Float64("shadow-x", defaults.ShadowOffset.X, "rock shadow horizontal offset in scene units (range -1 to 1)")
	shadowY := flag.Float64("shadow-y", defaults.ShadowOffset.Y, "rock shadow vertical offset in scene units (positive is down, range -1 to 1)")
	fog := flag.Bool("fog", true, "moving fog between background and foreground layers")
	bats := flag.Bool("bats", true, "occasional bats flying across the cave (interactive viewer only)")
	view := flag.String("view", "shaded", "terrain view: shaded, clay, height, normals, shadows")
	hover := flag.Bool("hover", false, "highlight foreground rocks and guide lines under the mouse")
	tolerance := flag.Float64("collision-tolerance", 0, "collision polygon simplification tolerance in scene units (one section is 1 by 1)")
	flag.Parse()
	config := defaults
	config.Seed, config.Texture = *seed, *texture
	config.BackgroundBlur, config.ShadowOpacity, config.ShadowBlur = *backgroundBlur, *shadowOpacity, *shadowBlur
	config.ShadowOffset = infinicave.V{X: *shadowX, Y: *shadowY}
	config.Fog = *fog
	config.Bats = *bats && *output == ""
	parsedView, err := infinicave.ParseView(*view)
	if err != nil {
		return err
	}
	config.View = parsedView
	config.CollisionTolerance = *tolerance
	scene, err := infinicave.NewScene(config)
	if err != nil {
		return err
	}
	defer scene.Close()
	g := &Game{scene: scene, seed: *seed, output: *output}
	if *hover && *output == "" {
		g.highlight, err = newHoverRenderer()
		if err != nil {
			return fmt.Errorf("compile hover shader: %w", err)
		}
		defer g.highlight.close()
	}

	_, displayHeight := ebiten.Monitor().Size()
	windowHeight := displayHeight * 4 / 5
	ebiten.SetWindowSize(windowHeight*9/16, windowHeight)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetWindowTitle("ebiten-infinicave | Scroll / Up / Down | R: regenerate")
	return ebiten.RunGame(g)
}
