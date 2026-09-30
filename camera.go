package main

import (
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// World Y is negative above the starting floor at zero. Target can travel
// upward without a limit; Y follows once its surrounding sections are ready.
type Camera struct {
	Y, Target float64
	Height    int
}

func (c *Camera) scroll(delta float64) {
	c.Target = math.Min(c.Target+delta, -float64(c.Height))
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	height := max(1, int(math.Round(float64(W)*float64(outsideHeight)/float64(max(1, outsideWidth)))))
	if g.output != "" {
		height = exportHeight
	}
	if g.camera.Height == 0 {
		g.camera.Y, g.camera.Target = -float64(height), -float64(height)
	}
	g.camera.Height = height
	g.camera.Y = math.Min(g.camera.Y, -float64(height))
	g.camera.scroll(0)
	return W, height
}

func (g *Game) updateCamera() {
	if g.output != "" {
		return
	}
	_, wheel := ebiten.Wheel()
	delta := -wheel * 48
	step := 480 / float64(ebiten.TPS())
	if ebiten.IsKeyPressed(ebiten.KeyArrowUp) || ebiten.IsKeyPressed(ebiten.KeyW) {
		delta -= step
	}
	if ebiten.IsKeyPressed(ebiten.KeyArrowDown) || ebiten.IsKeyPressed(ebiten.KeyS) {
		delta += step
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyPageUp) {
		delta -= float64(g.camera.Height) * .9
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyPageDown) {
		delta += float64(g.camera.Height) * .9
	}
	g.camera.scroll(delta)
	if inpututil.IsKeyJustPressed(ebiten.KeyHome) || inpututil.IsKeyJustPressed(ebiten.KeyEnd) {
		g.camera.Target = -float64(g.camera.Height)
	}
}
