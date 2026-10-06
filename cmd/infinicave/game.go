package main

import (
	"fmt"
	"image/png"
	"os"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/razzie/ebiten-infinicave"
)

const (
	renderWidth  = 1000
	exportHeight = 2400
)

type Game struct {
	camera       Camera
	scene        *infinicave.Scene
	loading      bool
	seed         int64
	output       string
	highlight    *hoverRenderer
	exported     bool
	exportErr    error
	carving      carveGesture
	carveStatus  string
	screenWidth  int
	screenHeight int
	regenerate   bool
}

func (g *Game) viewport() infinicave.Viewport {
	return infinicave.Viewport{Y: g.camera.Y, Height: g.camera.Height, Velocity: g.camera.Velocity}
}

func (g *Game) Update() error {
	if g.exported {
		if g.exportErr != nil {
			return g.exportErr
		}
		return ebiten.Termination
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyR) {
		g.seed++
		g.regenerate = true
	}
	if g.regenerate {
		g.scene.Reset(g.seed)
		g.carving = carveGesture{}
		g.carveStatus = ""
		g.regenerate = false
	}
	g.scene.SetRenderWidth(g.screenWidth)
	g.updateCamera()
	g.camera.step()
	g.loading = !g.scene.Update(g.viewport())
	g.updateCarving()
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	g.scene.Draw(screen, g.viewport())
	g.drawHover(screen)
	if g.loading {
		ebitenutil.DebugPrintAt(screen, "Growing upward...", 8, 24)
	}
	if g.output == "" {
		g.drawCarving(screen)
		ebitenutil.DebugPrintAt(screen, fmt.Sprintf("FPS: %.1f", ebiten.ActualFPS()), 8, 8)
	}
	if g.output != "" && !g.exported && !g.loading {
		g.exported = true
		img := ebiten.NewImage(renderWidth, exportHeight)
		defer img.Deallocate()
		height := float64(exportHeight) / renderWidth * infinicave.Width
		g.scene.Draw(img, infinicave.Viewport{Y: -height, Height: height})
		f, err := os.Create(g.output)
		if err != nil {
			g.exportErr = err
			return
		}
		g.exportErr = png.Encode(f, img)
		if err := f.Close(); g.exportErr == nil {
			g.exportErr = err
		}
	}
}
