package terrain

import (
	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// Section holds generated geometry plus one section of padding on each side
// along the scrolling axis. Geometry uses ordinary X/Y scene units, local to
// the owned square [0, 1]². Add Origin to local points to get world coordinates.
// Padding spans local Y = -1 to 2 in Vertical scenes or local X = -1 to 2 in
// Horizontal scenes. Min/Max describe the owned world square; WindowOrigin
// starts the padded window. Vegetation may extend into padding and should be
// drawn once per owning section. Background cells extend from -0.5 to 1.5
// across the bounded axis. Top and WindowTop are legacy Vertical metadata.
//
// Each generated Section owns its slices; callers may modify them. Rock grids
// describe visual faces; Collision contains boundaries for physics integration.
type Section struct {
	Orientation Orientation
	// Origin converts section-local points to world coordinates by addition.
	// Min/Max bound the owned square; WindowOrigin starts the padded window.
	Origin, WindowOrigin, Min, Max geom.V
	// ID is 0 at the starting edge, then -1, -2, ... upward or rightward.
	// Top is ID-1 in Vertical scenes and zero in Horizontal scenes.
	ID                     int64
	Top, WindowTop         float64
	Background, Foreground RockGrid
	Vines, ForegroundVines []Vine
	Mushrooms              []MushroomGroup
	Guides                 []Guide
	Holes                  []Hole
	// Collision contains foreground rock boundaries in world coordinates.
	Collision CollisionGeometry
}

func SectionTop(id int64) float64 { return -float64(id+1) * SectionHeight }

func SectionWindowTop(id int64) float64 { return SectionTop(id) - SectionHeight }
