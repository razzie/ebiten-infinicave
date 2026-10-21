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
	mode         infinicave.Orientation
	camera       Camera
	scene        *infinicave.Scene
	loading      bool
	seed         int64
	output       string
	fogMouse     bool
	highlight    *hoverRenderer
	ribbon       *ribbonTrail
	exported     bool
	exportErr    error
	carving      carveGesture
	carveStatus  string
	screenWidth  int
	screenHeight int
	regenerate   bool

	carveStatusRemaining float64
}

func (g *Game) viewport() infinicave.Viewport {
	if g.mode == infinicave.Horizontal {
		return infinicave.Viewport{X: -(g.camera.Y + g.camera.Height), Width: g.camera.Height, Velocity: -g.camera.Velocity}
	}
	return infinicave.Viewport{Y: g.camera.Y, Height: g.camera.Height, Velocity: g.camera.Velocity}
}

func (g *Game) Update() error {
	if g.exported {
		if g.exportErr != nil {
			return g.exportErr
		}
		return ebiten.Termination
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		return ebiten.Termination
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyF11) ||
		(ebiten.IsKeyPressed(ebiten.KeyAlt) && inpututil.IsKeyJustPressed(ebiten.KeyEnter)) {
		// Ebitengine restores the previous window size when leaving fullscreen.
		ebiten.SetFullscreen(!ebiten.IsFullscreen())
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyR) {
		g.seed++
		g.regenerate = true
	}
	if g.regenerate {
		g.scene.Reset(g.seed)
		g.carving = carveGesture{}
		if g.ribbon != nil {
			g.ribbon.points = g.ribbon.points[:0]
		}
		g.setCarveStatus("")
		g.regenerate = false
	}
	g.scene.SetRenderWidth(g.renderPixels())
	g.updateCamera()
	g.camera.step()
	g.updateFogVelocity()
	g.loading = !g.scene.Update(g.viewport())
	g.updateCarving()
	g.updateRibbon()
	return nil
}

func (g *Game) updateFogVelocity() {
	if !g.fogMouse || g.output != "" {
		return
	}
	velocity := infinicave.V{}
	if ebiten.IsFocused() {
		x, y := ebiten.CursorPositionF()
		velocity = g.mouseFogVelocity(infinicave.V{X: x, Y: y})
	}
	g.scene.SetFogVelocity(velocity)
}

func (g *Game) mouseFogVelocity(cursor infinicave.V) infinicave.V {
	if g.screenWidth <= 0 || g.screenHeight <= 0 {
		return infinicave.V{}
	}
	center := infinicave.V{X: float64(g.screenWidth) / 2, Y: float64(g.screenHeight) / 2}
	// One scale for both axes keeps flow aimed at the pointer on any aspect
	// ratio. Half the shorter screen dimension gives 0.1 scene units/second.
	return cursor.Sub(center).Mul(.2 / float64(g.renderPixels()))
}

func (g *Game) Draw(screen *ebiten.Image) {
	passes := infinicave.RenderPasses{}
	if g.ribbon != nil {
		passes = g.ribbon.passes()
	}
	g.scene.DrawWithPasses(screen, g.viewport(), passes)
	g.drawHover(screen)
	if g.loading {
		message := "Growing upward..."
		foregroundReady := g.scene.Readiness(g.viewport()).Foreground
		if foregroundReady {
			message = "Finishing cave details..."
		}
		if g.mode == infinicave.Horizontal && !foregroundReady {
			message = "Growing rightward..."
		}
		ebitenutil.DebugPrintAt(screen, message, 8, 24)
	}
	if g.output == "" {
		g.drawCarving(screen)
		ebitenutil.DebugPrintAt(screen, fmt.Sprintf("FPS: %.1f", ebiten.ActualFPS()), 8, 8)
	}
	if g.output != "" && !g.exported && !g.loading {
		g.exported = true
		width, height := g.exportSize()
		img := ebiten.NewImage(width, height)
		defer img.Deallocate()
		g.scene.Draw(img, g.viewport())
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

func (g *Game) exportSize() (int, int) {
	if g.mode == infinicave.Horizontal {
		return exportHeight, renderWidth
	}
	return renderWidth, exportHeight
}
