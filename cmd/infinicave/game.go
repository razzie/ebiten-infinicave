package main

import (
	"image/png"
	"os"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/razzie/ebiten-infinicave"
)

const exportHeight = 2400

type Game struct {
	camera    Camera
	scene     *infinicave.Scene
	loading   bool
	seed      int64
	output    string
	hover     bool
	exported  bool
	exportErr error
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
		g.scene.Reset(g.seed)
	}
	g.updateCamera()
	g.camera.step()
	g.loading = !g.scene.Update(g.viewport())
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	g.scene.Draw(screen, g.viewport())
	if g.hover && ebiten.IsFocused() {
		x, y := ebiten.CursorPositionF()
		g.scene.DrawHover(screen, g.viewport(), x, y)
	}
	if g.loading {
		ebitenutil.DebugPrint(screen, "Growing upward...")
	}
	if g.output != "" && !g.exported && !g.loading {
		g.exported = true
		img := ebiten.NewImage(infinicave.Width, exportHeight)
		defer img.Deallocate()
		g.scene.Draw(img, infinicave.Viewport{Y: -exportHeight, Height: exportHeight})
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
