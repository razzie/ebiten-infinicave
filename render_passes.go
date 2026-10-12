package infinicave

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
)

// RenderPass draws custom content once per valid DrawWithPasses call, on the
// game goroutine, including while terrain is loading and in diagnostic views.
// A pass must only draw: do not update, reset, carve, or close the Scene here.
type RenderPass func(RenderContext)

// RenderPasses inserts custom content into the scene's back-to-front order.
// Nil callbacks skip their passes. Passes do not participate in background
// blur or shadow masks. Bats draw after both passes and all decorations.
type RenderPasses struct {
	// BehindRocks runs after background effects and fog, before foreground rocks.
	BehindRocks RenderPass
	// AboveRocks runs after rocks, mushrooms, webs, and foreground vines,
	// before all grass ground patches and blades.
	AboveRocks RenderPass
}

// RenderContext maps public world coordinates to the current drawing target.
// Horizontal scenes draw into a temporary rotated target; always use
// WorldToTarget rather than assuming Dst has the final screen orientation.
type RenderContext struct {
	// Dst is borrowed for this callback; do not retain or deallocate it.
	Dst *ebiten.Image
	// Viewport uses public world axes, adjusted to the renderer's pixel alignment.
	Viewport    Viewport
	Orientation Orientation
	// WorldToTarget includes camera translation, native scale, centering, and
	// orientation. Concat it after a sprite's own placement in world units.
	WorldToTarget ebiten.GeoM
	// Min and Max bound the whole target in world units, including side margins.
	Min, Max V
}

func newRenderContext(dst *ebiten.Image, viewport Viewport, orientation Orientation, y float64) RenderContext {
	view := render.TargetTransform(dst)
	y = render.RenderAlignedY(y, view.Pixels)
	c := RenderContext{Dst: dst, Viewport: viewport, Orientation: orientation}
	if orientation == Horizontal {
		c.Viewport.X = -(y + viewport.Width)
		// (world X, world Y) -> (world Y, -world X) in the section frame.
		c.WorldToTarget.SetElement(0, 0, 0)
		c.WorldToTarget.SetElement(0, 1, 1)
		c.WorldToTarget.SetElement(1, 0, -1)
		c.WorldToTarget.SetElement(1, 1, 0)
	} else {
		c.Viewport.Y = y
	}
	c.WorldToTarget.Translate(0, -y)
	c.WorldToTarget.Scale(float64(view.Pixels), float64(view.Pixels))
	c.WorldToTarget.Translate(view.OffsetX, 0)
	inverse := c.WorldToTarget
	inverse.Invert()
	bounds := dst.Bounds()
	x0, y0 := inverse.Apply(float64(bounds.Min.X), float64(bounds.Min.Y))
	x1, y1 := inverse.Apply(float64(bounds.Max.X), float64(bounds.Max.Y))
	c.Min, c.Max = V{X: min(x0, x1), Y: min(y0, y1)}, V{X: max(x0, x1), Y: max(y0, y1)}
	return c
}
