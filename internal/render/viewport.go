package render

import (
	"math"
)

// Viewport describes the region to render. Vertical scenes use Y and Height,
// with Y <= -Height and a starting viewport at Y = -Height. Horizontal scenes
// use X and Width, with X >= 0 and a starting viewport at X = 0. The other
// axis spans [0, 1]. Velocity is movement along world Y or X in scene units
// per tick and controls prefetching; the caller owns camera movement.
type Viewport struct {
	Y        float64
	Height   float64
	Velocity float64
	// Horizontal scenes use X and Width instead of Y and Height. X is the
	// left edge, must be nonnegative, and positive Velocity moves rightward.
	X, Width float64
}

func ValidViewport(v Viewport) bool {
	return v.Height > 0 && !math.IsNaN(v.Height) && !math.IsInf(v.Height, 0) &&
		!math.IsNaN(v.Y) && !math.IsInf(v.Y, 0) &&
		v.Y <= -v.Height && !math.IsNaN(v.Velocity) && !math.IsInf(v.Velocity, 0)
}
