package infinicave

import (
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

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
	select {
	case ready := <-w.terrain:
		geometry := ready.geometry
		for _, cut := range w.cuts {
			geometry, _, _ = cut.Geometry(ready.id, geometry, g.collisionTolerance)
		}
		if w.collision == nil {
			w.collision = make(map[int64]*terrain.Geometry)
		}
		w.collision[ready.id] = geometry
		w.collisionRevision++
		if g.onCollisionReady != nil {
			g.onCollisionReady(terrain.OrientedCollision(g.orientation, geometry.Collision))
		}
	default:
	}
}
