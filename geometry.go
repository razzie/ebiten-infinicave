package infinicave

import (
	"math/rand"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// V is a point or vector in scene units.
type V = geom.V

// Screen Y increases downward; positive Z points toward the camera.
type V3 = geom.V3

// Guide is a sampled terrain guide with cumulative arc lengths and bounds.
type Guide = terrain.Guide

// Projection describes a point's nearest position and frame along a guide.
type Projection = terrain.Projection

// BranchSegment carries a tapering raised spur through the existing cells.
// LightA/B are the historical strength values, now used as relief amplitude.
type BranchSegment = terrain.BranchSegment

// BranchField buckets segments by padded bounds; a spur contributes nothing
// outside its support, and its relief bias is independent of segment order.
type BranchField = terrain.BranchField

// RockCell is shared by rendering and vine growth; colors belong to entire
// Voronoi faces, not a second approximation of the original noise field.
type RockCell = terrain.RockCell

// RockGrid holds the visual faces of a generated rock layer.
type RockGrid = terrain.RockGrid

// VinePoint is a sampled position and radius along a vine.
type VinePoint = terrain.VinePoint

// Vine holds a generated stem and its attachment to a parent stem.
type Vine = terrain.Vine

// A small CPU field composites both grids in drawing order. Its clearance
// measures distance from light faces and black voids, including vine width.
type VineTerrain = terrain.VineTerrain

// Mushroom holds the geometry and material of one generated mushroom.
type Mushroom = terrain.Mushroom

// MushroomGroup holds mushrooms generated together along a guide.
type MushroomGroup = terrain.MushroomGroup

// Perlin supplies classic gradient Perlin noise.
type Perlin = terrain.Perlin

// NewPerlin initializes a noise field using the supplied random stream.
func NewPerlin(rng *rand.Rand) *Perlin { return terrain.NewPerlin(rng) }
