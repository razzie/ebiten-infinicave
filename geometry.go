package infinicave

import (
	"context"
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

// VineRoute identifies free growth, cell edges, cell centers, or guide following.
type VineRoute = terrain.VineRoute

const (
	FreeVineRoute       = terrain.FreeVineRoute
	CellEdgeVineRoute   = terrain.CellEdgeVineRoute
	CellCenterVineRoute = terrain.CellCenterVineRoute
	GuideVineRoute      = terrain.GuideVineRoute
)

// VineStyle describes a vine family's hues and transition distances.
type VineStyle = terrain.VineStyle

// VineHue selects a pastel vine material.
type VineHue = terrain.VineHue

const (
	VineSage  = terrain.VineSage
	VineStraw = terrain.VineStraw
)

// VineLeaf is a pointed leaf attached at an original stem distance.
type VineLeaf = terrain.VineLeaf

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

// Section holds generated geometry plus one section of padding on each side
// along the scrolling axis. Geometry uses ordinary X/Y scene units, local to
// the owned square [0, 1]². Add Origin to local points to get world coordinates.
// Padding spans local Y = -1 to 2 in Vertical scenes or local X = -1 to 2 in
// Horizontal scenes. Min/Max describe the owned world square; WindowOrigin
// starts the padded window. Vegetation may extend into padding and should be
// drawn once per owning section. Background cells and vines stay within -0.5 to 1.5
// across the bounded axis. Top and WindowTop are legacy Vertical metadata.
//
// Each generated Section owns its slices; callers may modify them. Rock grids
// describe visual faces; Collision contains boundaries for physics integration.
type Section = terrain.Section

// SectionContent supplies authored content for a square cave section. Fields
// use section-local scene units, with the owned square spanning (0, 0) to (1, 1).
// The structure can grow to include other authored objects in future versions.
// Empty Guides produces no procedural foreground. Holes remove foreground rock
// after shaping. Hole bounds must fit -1 to 2 along the scrolling axis,
// including seam crossings (Y for Vertical, X for Horizontal);
// declare longer cuts as multiple section-local holes. Invalid holes are ignored.
type SectionContent = terrain.SectionContent

// SectionLoader loads content for one square section. IDs start at 0 at the
// starting edge and decrease in the direction of growth: -1, -2, ... . Points
// use ordinary section-local X/Y in the configured orientation.
// The loader owns its returned slices;
// generation copies them and recomputes guide S, Min, and Max.
// BrightSign defaults to 1; use -1 to reverse the lit side. A zero Seed receives
// a stable seed based on the world seed, section ID, and guide's slice index.
// Guides with nonfinite coordinates or fewer than two distinct consecutive
// points are ignored. Returning empty content does not request random guides.
//
// Generation also loads neighboring sections for seamless padding. IDs may be
// requested repeatedly and in any order, so return consistent content per ID.
// Scene serializes loader calls within one world and copies returned content
// before the next call. Synchronous generation calls it on the calling goroutine.
// Reset can overlap an old worker,
// so loaders sharing mutable state must be safe for concurrent calls.
type SectionLoader = terrain.SectionLoader

// GenerateSection synchronously generates a full cave section without
// allocating GPU resources. It uses Vertical orientation. ID 0 is the bottom
// section; IDs -1, -2, ... grow upward. Use GenerateSectionWithConfig for Horizontal.
// The same seed and ID reproduce the same geometry, independently of load order.
// Positive IDs return an error. Generation is expensive; use a background
// goroutine when calling from a game. This package still depends on Ebitengine,
// whose initialization requires a graphical environment on desktop platforms.
func GenerateSection(seed, id int64) (Section, error) {
	return GenerateSectionWithConfig(Config{Seed: seed}, id)
}

// GenerateSectionWithConfig generates a section using the same seed,
// collision tolerance, and section content loader as a Scene. Texture,
// background effects, moss, grass and hanging growth density, and View only affect
// rendering. A custom loader must return the same content for repeated IDs to preserve geometry across neighboring
// sections and cache eviction.
// OnCollisionReady, when set, runs on the caller's goroutine before background, shading, or vegetation
// generation and receives an independent copy of the section's collision.
func GenerateSectionWithConfig(config Config, id int64) (Section, error) {
	if err := config.validate(); err != nil {
		return Section{}, err
	}
	return terrain.GenerateSection(terrain.GenerationOptions{
		Shape:              config.shapeOptions(),
		Seed:               config.Seed,
		Orientation:        config.Orientation,
		ForceGuidesUp:      config.ForceGuidesUp,
		MushroomDensity:    config.MushroomDensity,
		CollisionTolerance: config.CollisionTolerance,
		SpiderWebMaxSize:   config.SpiderWebMaxSize,
		SpiderWebDensity:   config.SpiderWebDensity,
		LoadSection:        config.LoadSection,
		OnCollisionReady:   config.OnCollisionReady,
	}, id)
}

// CollisionGeometry contains the union boundaries of foreground rock in world
// coordinates in scene units. Polygons are closed implicitly and may be concave.
// Outer loops have positive signed area; holes have negative signed area. Physics engines
// requiring convex fixtures must decompose these loops and account for holes.
//
// Geometry includes a section of padding on either side along the scrolling
// axis. Use only the owned square Min/Max when combining adjacent sections.
// Background rock, vines, mushrooms, and decorative bevels are not solid.
type CollisionGeometry = terrain.CollisionGeometry

// CollisionGeometry returns a copy of a cached section's collision boundaries.
// Geometry becomes available before vegetation generation and render uploads.
// Missing, evicted, and closed sections return false.
// IDs are 0 at the starting edge, then -1, -2, ... in the direction of growth;
// positive IDs return false.
// Call after Update; retain the returned copy as long as your game needs it.
func (g *Scene) CollisionGeometry(id int64) (CollisionGeometry, bool) {
	if g.closed || id > 0 || id == -1<<63 {
		return CollisionGeometry{}, false
	}
	section := g.world.sections[-id]
	if section != nil && section.geometry != nil {
		return terrain.OrientedCollision(g.orientation, section.geometry.Collision), true
	}
	if geometry := g.world.collision[-id]; geometry != nil {
		return terrain.OrientedCollision(g.orientation, geometry.Collision), true
	}
	return CollisionGeometry{}, false
}

// Drain early terrain independently of mesh reception and GPU upload budgets.
// The generation worker never invokes application code on its goroutine.
func (w *world) receiveCollision(g *Scene) {
	for {
		select {
		case ready := <-w.terrain:
			if !w.matchesCollision(ready.id, ready.request) ||
				ready.ctx != nil && ready.ctx.Err() != nil && context.Cause(ready.ctx) != errSectionComplete {
				continue
			}
			geometry := ready.geometry
			for _, index := range w.cutsForSection(ready.id, 0) {
				cut := w.cuts[index].expand()
				geometry, _, _ = cut.Geometry(ready.id, geometry, g.collisionTolerance)
			}
			if w.collision == nil {
				w.collision = make(map[int64]*terrain.Geometry)
			}
			geometry = geometry.WithoutGuides(w.retiredGuides)
			w.collision[ready.id] = geometry
			w.collisionRevision++
			if g.onCollisionReady != nil {
				g.onCollisionReady(terrain.OrientedCollision(g.orientation, geometry.Collision))
				if g.closed || g.world != w {
					return
				}
			}
		default:
			return
		}
	}
}
