package main

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"
	infinicave "github.com/razzie/ebiten-infinicave"
)

const (
	drillWidth         = .02
	drillMinWidth      = .002
	blastStartRadius   = .012
	blastMinRadius     = .001
	blastLockRadius    = .02
	blastLockSeconds   = .2
	blastGrowthRate    = math.Ln2 / 2 // Growth speed doubles every two seconds.
	blastInitialGrowth = .02          // Scene units per second at the start.
	blastGrowthOffset  = blastInitialGrowth/blastGrowthRate - blastStartRadius
	carveWheelScale    = 1.1 // Each upward notch enlarges the cut by 10%; down reverses it.
	drillDragPixels    = 8
	carveStatusSeconds = 2
)

type carveGesture struct {
	active, segment            bool
	originCursor, origin, end  infinicave.V
	radius                     float64
	width                      float64
	heldSeconds                float64
	growthStopped, blastLocked bool
}

// A drag beyond the cursor dead zone latches segment mode before the blast reaches
// its lock radius or hold duration, even if the cursor returns to its original
// pixel. After the hold duration, the blast follows the cursor. Camera motion
// alone does not turn a blast into a segment.
func (c *carveGesture) advance(cursor, world infinicave.V, pressed, released, cancel bool, seconds, wheel float64) bool {
	if cancel {
		*c = carveGesture{}
		return false
	}
	if pressed {
		*c = carveGesture{active: true, originCursor: cursor, origin: world, end: world, radius: blastStartRadius, width: drillWidth}
	}
	if !c.active {
		return false
	}
	c.blastLocked = c.blastLocked || c.radius >= blastLockRadius || c.heldSeconds >= blastLockSeconds
	if cursor.Sub(c.originCursor).Len2() >= drillDragPixels*drillDragPixels && !c.blastLocked {
		c.segment = true
	}
	if c.segment {
		c.end = world
		if wheel != 0 {
			c.width = max(drillMinWidth, c.width*math.Pow(carveWheelScale, wheel))
		}
	} else {
		if wheel != 0 {
			c.radius = max(blastMinRadius, c.radius*math.Pow(carveWheelScale, wheel))
			// Downward scrolling latches manual radius control until the next press.
			c.growthStopped = c.growthStopped || wheel < 0
		}
		if !c.growthStopped {
			c.radius += (c.radius + blastGrowthOffset) * math.Expm1(blastGrowthRate*seconds)
		}
		c.heldSeconds += seconds
		c.blastLocked = c.blastLocked || c.radius >= blastLockRadius || c.heldSeconds >= blastLockSeconds
		if c.heldSeconds >= blastLockSeconds {
			c.origin, c.end = world, world
		}
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

func (g *Game) setCarveStatus(status string) {
	g.carveStatus = status
	g.carveStatusRemaining = 0
	if status != "" {
		g.carveStatusRemaining = carveStatusSeconds
	}
}

func (g *Game) updateCarving() {
	if g.output != "" {
		return
	}
	seconds := 1 / float64(ebiten.TPS())
	if g.carveStatusRemaining > 0 {
		g.carveStatusRemaining = max(0, g.carveStatusRemaining-seconds)
		if g.carveStatusRemaining == 0 {
			g.setCarveStatus("")
		}
	}
	x, y := ebiten.CursorPositionF()
	cursor := infinicave.V{X: x, Y: y}
	cancel := inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonRight) || !ebiten.IsFocused()
	wasActive := g.carving.active
	_, wheel := ebiten.Wheel()
	commit := g.carving.advance(cursor, g.worldPoint(cursor),
		inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft),
		inpututil.IsMouseButtonJustReleased(ebiten.MouseButtonLeft), cancel, seconds, wheel)
	if cancel && wasActive {
		g.setCarveStatus("Cut cancelled")
	}
	if !commit {
		return
	}
	var result infinicave.CarveResult
	var err error
	if g.carving.segment {
		if g.carving.origin == g.carving.end {
			g.setCarveStatus("Cut has no length")
			return
		}
		result, err = g.scene.CarveSegment(g.carving.origin, g.carving.end, g.carving.width)
	} else {
		result, err = g.scene.CarveCircle(g.carving.origin, g.carving.radius)
	}
	if err != nil {
		g.setCarveStatus(err.Error())
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
	status := fmt.Sprintf("Cut: %d formations affected, %d split, %d destroyed", len(result.Changes), splits, destroyed)
	if !result.Complete {
		status += " (unloaded terrain: partial result)"
	}
	g.setCarveStatus(status)
}

func (g *Game) drawCarving(screen *ebiten.Image) {
	height := screen.Bounds().Dy()
	ebitenutil.DebugPrintAt(screen, "LMB: hold for blast / drag for drill; release to carve | Wheel: resize; down stops auto growth | RMB: cancel", 8, height-20)
	if g.carveStatus != "" {
		ebitenutil.DebugPrintAt(screen, g.carveStatus, 8, height-36)
	}
	c := g.carving
	if !c.active {
		return
	}
	scale := float64(g.renderPixels()) / infinicave.Width
	pixel := g.screenPoint
	x, y := pixel(c.origin)
	clr := color.NRGBA{R: 255, G: 185, B: 95, A: 210}
	if !c.segment {
		vector.StrokeCircle(screen, x, y, float32(c.radius*scale), 2, clr, true)
		return
	}
	if c.origin == c.end {
		return
	}
	normal := c.end.Sub(c.origin).Norm().Perp().Mul(c.width / 2)
	points := []infinicave.V{c.origin.Sub(normal), c.end.Sub(normal), c.end.Add(normal), c.origin.Add(normal)}
	for i, p := range points {
		x1, y1 := pixel(p)
		x2, y2 := pixel(points[(i+1)%len(points)])
		vector.StrokeLine(screen, x1, y1, x2, y2, 2, clr, true)
	}
}
