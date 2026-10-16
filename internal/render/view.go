package render

import (
	"fmt"
	"math"
)

// View selects the rendered material or a diagnostic view.
type View int

const (
	ViewShaded  View = iota // Full materials and vegetation.
	ViewClay                // Neutral rock material.
	ViewHeight              // Rock height as grayscale.
	ViewNormals             // Surface normals as RGB.
	ViewShadows             // Shadow visibility as grayscale.
)

func (v View) String() string {
	switch v {
	case ViewShaded:
		return "shaded"
	case ViewClay:
		return "clay"
	case ViewHeight:
		return "height"
	case ViewNormals:
		return "normals"
	case ViewShadows:
		return "shadows"
	default:
		return fmt.Sprintf("View(%d)", int(v))
	}
}

// ParseView converts a viewer flag to a View. Empty means shaded.
func ParseView(value string) (View, error) {
	switch value {
	case "", "shaded":
		return ViewShaded, nil
	case "clay":
		return ViewClay, nil
	case "height":
		return ViewHeight, nil
	case "normals":
		return ViewNormals, nil
	case "shadows":
		return ViewShadows, nil
	default:
		return ViewShaded, fmt.Errorf("infinicave: unknown view %q", value)
	}
}

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
