package main

import (
	"fmt"
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	infinicave "github.com/razzie/ebiten-infinicave"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	options, err := parseOptions()
	if err != nil {
		return err
	}
	config := options.config
	orientation := config.Orientation
	scene, err := infinicave.NewScene(config)
	if err != nil {
		return err
	}
	defer scene.Close()
	g := &Game{mode: orientation, scene: scene, seed: config.Seed, output: options.output}
	if options.hover && options.output == "" {
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
