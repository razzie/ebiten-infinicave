package main

import (
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/razzie/ebiten-infinicave"
)

const (
	cameraFriction = 0.92 // per tick; a velocity v coasts about v/(1-friction) units
	cameraMaxSpeed = .15
	cameraGlide    = 0.1
)

// The camera uses the shared section frame: negative Y moves forward along
// either scrolling axis. Game.viewport maps it to public world Y or X. Y moves
// every tick by Velocity, even while terrain loads. Height is the visible
// scrolling extent; Target is the destination of a glide (Home/End).
type Camera struct {
	Y, Target, Velocity float64
	Gliding             bool
	Height              float64
}

// Match the scene's native raster origin for all viewer interactions.
func viewerCameraY(y float64, pixels int) float64 {
	return math.Round(y*float64(pixels)) / float64(pixels)
}

func (c *Camera) floor() float64 { return -c.Height }

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
		if math.Abs(c.Target-c.Y) < .0005 {
			c.Y, c.Velocity, c.Gliding = c.Target, 0, false
		}
	} else {
		c.Velocity *= cameraFriction
		if math.Abs(c.Velocity) < .00002 {
			c.Velocity = 0
		}
	}
	c.Y += c.Velocity
	if c.Y >= c.floor() {
		c.Y, c.Velocity, c.Gliding = c.floor(), 0, false
	}
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	width, height := max(1, outsideWidth), max(1, outsideHeight)
	if g.output != "" {
		width, height = g.exportSize()
	}
	if g.screenWidth != width || g.screenHeight != height {
		g.carving = carveGesture{}
		g.screenWidth, g.screenHeight = width, height
	}
	extent := height
	if g.mode == infinicave.Horizontal {
		extent = width
	}
	sceneHeight := float64(extent) / float64(min(width, height)) * infinicave.Width
	if g.camera.Height == 0 {
		g.camera.Y, g.camera.Target = -sceneHeight, -sceneHeight
	}
	g.camera.Height = sceneHeight
	g.camera.Y = math.Min(g.camera.Y, -sceneHeight)
	g.camera.Target = math.Min(g.camera.Target, -sceneHeight)
	return width, height
}

// Ebitengine supplies window dimensions in device-independent pixels. Include
// the monitor scale so the drawing surface also stays native on HiDPI displays.
func (g *Game) LayoutF(outsideWidth, outsideHeight float64) (float64, float64) {
	return g.layoutNative(outsideWidth, outsideHeight, ebiten.Monitor().DeviceScaleFactor())
}

func (g *Game) layoutNative(width, height, scale float64) (float64, float64) {
	w, h := g.Layout(int(math.Ceil(width*scale)), int(math.Ceil(height*scale)))
	return float64(w), float64(h)
}

func (g *Game) updateCamera() {
	if g.output != "" {
		return
	}
	wheelX, wheel := ebiten.Wheel()
	if g.mode == infinicave.Horizontal && wheelX != 0 {
		wheel = -wheelX
	}
	// Reserve wheel input for carving size, including the initial press tick.
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) || g.carving.active {
		wheel = 0
	}
	// An impulse of d*(1-friction) coasts about d scene units in total.
	const gain = 1 - cameraFriction
	impulse := -wheel * .048 * gain
	accel := .48 / float64(ebiten.TPS()) * gain
	forward, backward := ebiten.KeyArrowUp, ebiten.KeyArrowDown
	forwardAlt, backwardAlt := ebiten.KeyW, ebiten.KeyS
	if g.mode == infinicave.Horizontal {
		forward, backward = ebiten.KeyArrowRight, ebiten.KeyArrowLeft
		forwardAlt, backwardAlt = ebiten.KeyD, ebiten.KeyA
	}
	if ebiten.IsKeyPressed(forward) || ebiten.IsKeyPressed(forwardAlt) {
		impulse -= accel
	}
	if ebiten.IsKeyPressed(backward) || ebiten.IsKeyPressed(backwardAlt) {
		impulse += accel
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyPageUp) {
		impulse -= g.camera.Height * .9 * gain
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyPageDown) {
		impulse += g.camera.Height * .9 * gain
	}
	if impulse != 0 {
		g.camera.push(impulse)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyHome) || inpututil.IsKeyJustPressed(ebiten.KeyEnd) {
		g.camera.glideTo(g.camera.floor())
	}
}

func (g *Game) renderPixels() int { return max(1, min(g.screenWidth, g.screenHeight)) }

func (g *Game) renderOffsetX() float64 {
	if g.mode == infinicave.Horizontal {
		return 0
	}
	return float64(g.screenWidth-g.renderPixels()) / 2
}

func (g *Game) renderOffsetY() float64 {
	if g.mode == infinicave.Horizontal {
		return float64(g.screenHeight-g.renderPixels()) / 2
	}
	return 0
}

func (g *Game) cameraX() float64 {
	return -(viewerCameraY(g.camera.Y, g.renderPixels()) + g.camera.Height)
}

func (g *Game) worldPoint(cursor infinicave.V) infinicave.V {
	if g.mode == infinicave.Horizontal {
		return infinicave.V{X: g.cameraX() + cursor.X/float64(g.renderPixels()), Y: (cursor.Y - g.renderOffsetY()) / float64(g.renderPixels())}
	}
	cursor.X -= g.renderOffsetX()
	return viewerWorldPoint(cursor, g.camera.Y, g.renderPixels())
}

func (g *Game) screenPoint(p infinicave.V) (float32, float32) {
	scale := float64(g.renderPixels())
	if g.mode == infinicave.Horizontal {
		return float32((p.X - g.cameraX()) * scale), float32(g.renderOffsetY() + p.Y*scale)
	}
	return float32(g.renderOffsetX() + p.X*scale), float32((p.Y - viewerCameraY(g.camera.Y, g.renderPixels())) * scale)
}
