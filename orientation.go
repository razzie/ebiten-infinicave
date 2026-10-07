package infinicave

import (
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Orientation selects the cave's fixed scrolling axis. Vertical grows upward
// from Y = 0; Horizontal grows rightward from X = 0. The bounded axis spans
// [0, 1]. Section IDs are 0, -1, -2, ... in the direction of growth.
type Orientation = terrain.Orientation

const Vertical = terrain.Vertical

const Horizontal = terrain.Horizontal

func orientedViewport(o Orientation, v Viewport) (Viewport, bool) {
	if o == Horizontal {
		// Validate X independently: adding Width must not hide a negative X.
		if !terrain.FiniteCarveValue(v.X) || v.X < 0 || !terrain.FiniteCarveValue(v.Width) || v.Width <= 0 {
			return Viewport{}, false
		}
		v = Viewport{Y: -(v.X + v.Width), Height: v.Width, Velocity: -v.Velocity}
	}
	return v, render.ValidViewport(v)
}
