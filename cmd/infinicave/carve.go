package main

import (
	"fmt"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"
	infinicave "github.com/razzie/ebiten-infinicave"
)

const drillWidth = .02

type carveGesture struct {
	active, segment           bool
	originCursor, origin, end infinicave.V
	radius                    float64
}

// Cursor movement latches segment mode, even if the cursor returns to its
// original pixel. Camera motion alone does not turn a blast into a segment.
func (c *carveGesture) advance(cursor, world infinicave.V, pressed, released, cancel bool, seconds float64) bool {
	if cancel {
		*c = carveGesture{}
		return false
	}
	if pressed {
		*c = carveGesture{active: true, originCursor: cursor, origin: world, end: world, radius: .012}
	}
	if !c.active {
		return false
	}
	if cursor != c.originCursor {
		c.segment = true
	}
	if c.segment {
		c.end = world
	} else {
		c.radius += .04 * seconds
	}
	if released {
		c.active = false
		return true
	}
	return false
}

func viewerWorldPoint(cursor infinicave.V, cameraY float64, screenWidth int) infinicave.V {
	// Match the scene's native raster origin; cursor scaling follows Layout.
	return cursor.Mul(float64(infinicave.Width) / float64(screenWidth)).
		Add(infinicave.V{Y: viewerCameraY(cameraY, screenWidth)})
}

func (g *Game) updateCarving() {
	if g.output != "" {
		return
	}
	x, y := ebiten.CursorPositionF()
	cursor := infinicave.V{X: x, Y: y}
	cancel := inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonRight) || !ebiten.IsFocused()
	wasActive := g.carving.active
	commit := g.carving.advance(cursor, viewerWorldPoint(cursor, g.camera.Y, g.screenWidth),
		inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft),
		inpututil.IsMouseButtonJustReleased(ebiten.MouseButtonLeft), cancel, 1/float64(ebiten.TPS()))
	if cancel && wasActive {
		g.carveStatus = "Cut cancelled"
	}
	if !commit {
		return
	}
	var result infinicave.CarveResult
	var err error
	if g.carving.segment {
		if g.carving.origin == g.carving.end {
			g.carveStatus = "Cut has no length"
			return
		}
		result, err = g.scene.CarveSegment(g.carving.origin, g.carving.end, drillWidth)
	} else {
		result, err = g.scene.CarveCircle(g.carving.origin, g.carving.radius)
	}
	if err != nil {
		g.carveStatus = err.Error()
		return
	}
	splits, destroyed := 0, 0
	for _, change := range result.Changes {
		if len(change.Remaining) > 1 {
			splits++
		} else if len(change.Remaining) == 0 {
			destroyed++
		}
	}
	g.carveStatus = fmt.Sprintf("Cut: %d formations affected, %d split, %d destroyed", len(result.Changes), splits, destroyed)
	if !result.Complete {
		g.carveStatus += " (unloaded terrain: partial result)"
	}
}

func (g *Game) drawCarving(screen *ebiten.Image) {
	text := "LMB: hold for blast / drag for drill; release to carve | RMB: cancel"
	if g.carveStatus != "" {
		text += "\n" + g.carveStatus
	}
	ebitenutil.DebugPrintAt(screen, text, 8, screen.Bounds().Dy()-36)
	c := g.carving
	if !c.active {
		return
	}
	scale := float64(screen.Bounds().Dx()) / infinicave.Width
	cameraY := viewerCameraY(g.camera.Y, screen.Bounds().Dx())
	pixel := func(p infinicave.V) (float32, float32) {
		return float32(p.X * scale), float32((p.Y - cameraY) * scale)
	}
	x, y := pixel(c.origin)
	clr := color.NRGBA{R: 255, G: 185, B: 95, A: 210}
	if !c.segment {
		vector.StrokeCircle(screen, x, y, float32(c.radius*scale), 2, clr, true)
		return
	}
	if c.origin == c.end {
		return
	}
	normal := c.end.Sub(c.origin).Norm().Perp().Mul(drillWidth / 2)
	points := []infinicave.V{c.origin.Sub(normal), c.end.Sub(normal), c.end.Add(normal), c.origin.Add(normal)}
	for i, p := range points {
		x1, y1 := pixel(p)
		x2, y2 := pixel(points[(i+1)%len(points)])
		vector.StrokeLine(screen, x1, y1, x2, y2, 2, clr, true)
	}
}
