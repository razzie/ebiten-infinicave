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
	mode := flag.String("mode", "portrait", "scrolling mode: portrait (vertical) or landscape (horizontal)")
	seed := flag.Int64("seed", rand.Int63(), "random seed (random by default)")
	output := flag.String("output", "", "save the first 2400 pixels along the scrolling axis as a PNG and exit")
	texture := flag.Float64("texture", 8, "surface texture strength (0 disables it, range 0-16)")
	backgroundBlur := flag.Float64("background-blur", defaults.BackgroundBlur, "background rock and vine blur in scene units (0 disables it, range 0-0.05)")
	shadowOpacity := flag.Float64("shadow-opacity", defaults.ShadowOpacity, "dynamic rock shadow opacity (0 disables it, range 0-1)")
	shadowBlur := flag.Float64("shadow-blur", defaults.ShadowBlur, "dynamic rock shadow softness in scene units (range 0-0.05)")
	shadowX := flag.Float64("shadow-x", defaults.ShadowOffset.X, "rock shadow horizontal offset in scene units (range -1 to 1)")
	shadowY := flag.Float64("shadow-y", defaults.ShadowOffset.Y, "rock shadow vertical offset in scene units (positive is down, range -1 to 1)")
	fog := flag.Bool("fog", true, "moving fog between background and foreground layers")
	batsPerMinute := flag.Float64("bats-per-minute", defaults.BatsPerMinute, "average bat arrivals per minute (0 disables them, interactive viewer only)")
	view := flag.String("view", "shaded", "terrain view: shaded, clay, height, normals, shadows")
	hover := flag.Bool("hover", false, "highlight foreground rocks and guide lines under the mouse")
	tolerance := flag.Float64("collision-tolerance", 0, "collision polygon simplification tolerance in scene units (one section is 1 by 1)")
	flag.Parse()
	config := defaults
	orientation, err := parseMode(*mode)
	if err != nil {
		return err
	}
	config.Orientation = orientation
	config.Seed, config.Texture = *seed, *texture
	config.BackgroundBlur, config.ShadowOpacity, config.ShadowBlur = *backgroundBlur, *shadowOpacity, *shadowBlur
	config.ShadowOffset = infinicave.V{X: *shadowX, Y: *shadowY}
	config.Fog = *fog
	config.BatsPerMinute = *batsPerMinute
	if *output != "" {
		config.BatsPerMinute = 0
	}
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
	g := &Game{mode: orientation, scene: scene, seed: *seed, output: *output}
	if *hover && *output == "" {
		g.highlight, err = newHoverRenderer()
		if err != nil {
			return fmt.Errorf("compile hover shader: %w", err)
		}
		defer g.highlight.close()
	}

	displayWidth, displayHeight := ebiten.Monitor().Size()
	windowWidth, windowHeight := windowSize(orientation, displayWidth, displayHeight)
	ebiten.SetWindowSize(windowWidth, windowHeight)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	controls := "Up / Down"
	if orientation == infinicave.Horizontal {
		controls = "Left / Right"
	}
	ebiten.SetWindowTitle("ebiten-infinicave | Scroll / " + controls + " | R: regenerate")
	return ebiten.RunGame(g)
}

func parseMode(mode string) (infinicave.Orientation, error) {
	switch mode {
	case "portrait":
		return infinicave.Vertical, nil
	case "landscape":
		return infinicave.Horizontal, nil
	default:
		return infinicave.Vertical, fmt.Errorf("unknown mode %q: use portrait or landscape", mode)
	}
}

func windowSize(mode infinicave.Orientation, displayWidth, displayHeight int) (int, int) {
	if mode == infinicave.Horizontal {
		width := max(1, displayWidth*4/5)
		return width, max(1, width*9/16)
	}
	height := max(1, displayHeight*4/5)
	return max(1, height*9/16), height
}
