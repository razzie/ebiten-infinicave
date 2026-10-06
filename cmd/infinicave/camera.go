package main

import (
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/razzie/ebiten-infinicave"
)

const (
	cameraFriction = 0.92 // per tick; a velocity v coasts about v/(1-friction) px
	cameraMaxSpeed = 150.0
	cameraGlide    = 0.1
)

// World Y is negative above the starting floor at zero. Y moves every tick by
// Velocity, whether or not the sections it reveals have been generated.
// Target is only the destination of a glide (Home/End).
type Camera struct {
	Y, Target, Velocity float64
	Gliding             bool
	Height              int
}

func (c *Camera) floor() float64 { return -float64(c.Height) }

// push adds momentum; negative moves up.
func (c *Camera) push(v float64) {
	c.Gliding = false
	c.Velocity = math.Max(-cameraMaxSpeed, math.Min(cameraMaxSpeed, c.Velocity+v))
}

// glideTo eases to y regardless of current momentum.
func (c *Camera) glideTo(y float64) {
	c.Target, c.Gliding = math.Min(y, c.floor()), true
}

func (c *Camera) step() {
	if c.Gliding {
		c.Velocity = (c.Target - c.Y) * cameraGlide
		if math.Abs(c.Target-c.Y) < .5 {
			c.Y, c.Velocity, c.Gliding = c.Target, 0, false
		}
	} else {
		c.Velocity *= cameraFriction
		if math.Abs(c.Velocity) < .02 {
			c.Velocity = 0
		}
	}
	c.Y += c.Velocity
	if c.Y >= c.floor() {
		c.Y, c.Velocity, c.Gliding = c.floor(), 0, false
	}
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	height := max(1, int(math.Round(float64(infinicave.Width)*float64(outsideHeight)/float64(max(1, outsideWidth)))))
	if g.output != "" {
		height = exportHeight
	}
	if g.camera.Height == 0 {
		g.camera.Y, g.camera.Target = -float64(height), -float64(height)
	}
	g.camera.Height = height
	g.camera.Y = math.Min(g.camera.Y, -float64(height))
	g.camera.Target = math.Min(g.camera.Target, -float64(height))
	return infinicave.Width, height
}

func (g *Game) updateCamera() {
	if g.output != "" {
		return
	}
	_, wheel := ebiten.Wheel()
	// An impulse of d*(1-friction) coasts about d pixels in total.
	const gain = 1 - cameraFriction
	impulse := -wheel * 48 * gain
	accel := 480 / float64(ebiten.TPS()) * gain
	if ebiten.IsKeyPressed(ebiten.KeyArrowUp) || ebiten.IsKeyPressed(ebiten.KeyW) {
		impulse -= accel
	}
	if ebiten.IsKeyPressed(ebiten.KeyArrowDown) || ebiten.IsKeyPressed(ebiten.KeyS) {
		impulse += accel
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyPageUp) {
		impulse -= float64(g.camera.Height) * .9 * gain
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyPageDown) {
		impulse += float64(g.camera.Height) * .9 * gain
	}
	if impulse != 0 {
		g.camera.push(impulse)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyHome) || inpututil.IsKeyJustPressed(ebiten.KeyEnd) {
		g.camera.glideTo(g.camera.floor())
	}
}
