package infinicave

import (
	"github.com/razzie/ebiten-infinicave/internal/render"
)

// View selects the rendered material or a diagnostic view.
type View = render.View

// Viewport describes the region to render. Vertical scenes use Y and Height,
// with Y <= -Height and a starting viewport at Y = -Height. Horizontal scenes
// use X and Width, with X >= 0 and a starting viewport at X = 0. The other
// axis spans [0, 1]. Velocity is movement along world Y or X in scene units
// per tick and controls prefetching; the caller owns camera movement.
type Viewport = render.Viewport

const (
	ViewShaded  = render.ViewShaded
	ViewClay    = render.ViewClay
	ViewHeight  = render.ViewHeight
	ViewNormals = render.ViewNormals
	ViewShadows = render.ViewShadows
)

// ParseView converts a viewer flag to a View. Empty means shaded.
func ParseView(value string) (View, error) { return render.ParseView(value) }
