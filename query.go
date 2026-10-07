package infinicave

import (
	"fmt"
	"math"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// TargetMask selects the objects a world query can hit.
type TargetMask uint8

const (
	TargetRock TargetMask = 1 << iota
	TargetGuide
)

// Ray uses world coordinates and scene units. Direction is normalized by Query;
// its magnitude does not affect the cast length. A zero Direction or zero
// MaxDistance performs a point query. MaxDistance must be finite and nonnegative.
type Ray struct {
	Origin, Direction V
	MaxDistance       float64
}

// QueryOptions controls a world query. Zero Targets selects rocks and guides.
// GuideRadius is a finite, nonnegative distance around each guide polyline:
// zero tests the line itself; .006 matches the viewer's hover selection margin.
// GuideRadius is not a radius for rock collision or a moving character.
type QueryOptions struct {
	Targets     TargetMask
	GuideRadius float64
}

// FormationID is an opaque, comparable reference to a connected rock formation.
// IDs belong to one Scene world and expire after Reset, Close, or eviction of
// all cached portions of the formation. They do not depend on render resolution.
// When newly loaded geometry joins formations, previous IDs remain fetchable
// as aliases while the joined formation stays cached.
// Carving replaces affected formation IDs; CarveResult maps them to their
// remaining parts. Unaffected formation and guide IDs stay valid.
type FormationID struct{ world, object uint64 }

// GuideID is an opaque, comparable reference to a guide polyline. It belongs to
// one Scene world and expires after Reset, Close, or eviction of all its copies.
type GuideID struct{ world, object uint64 }

// Hit describes a query intersection. Kind is TargetRock or TargetGuide; only
// the corresponding ID is set. Normal is a unit 2D surface normal, pointing out
// of solid rock or away from a guide's radius. An exact guide-line intersection
// uses a perpendicular facing against the ray. Normal is zero for point queries
// and casts starting inside a target. Distance is measured from Ray.Origin.
type Hit struct {
	Kind          TargetMask
	FormationID   FormationID
	GuideID       GuideID
	Point, Normal V
	Distance      float64
	StartedInside bool
}

// QueryResult distinguishes a hit, a confirmed miss, and unloaded terrain.
// Complete means terrain along the ray up to the hit (or the full cast on a
// miss) was available. If unloaded terrain precedes a candidate hit, Found and
// Complete are both false: that candidate cannot be confirmed as nearest.
type QueryResult struct {
	Hit      Hit
	Found    bool
	Complete bool
}

// Formation contains exact union boundaries of the currently cached portion
// of a connected rock formation, in world coordinates and scene units. Loops
// are implicitly closed and may be concave. Outer loops have positive signed
// area; holes have negative signed area. Polygons are independent of
// CollisionTolerance. SectionIDs list the contributing owned section bands.
// Complete is false if the formation continues into uncached terrain; in that
// case Polygons include closing edges at the loaded bands' limits.
// Geometry can grow or shrink under the same ID as sections load or evict.
// Use Scene.GeometryRevision to detect when a retained copy may be stale.
// No individual face, shading, or generation details are exposed.
type Formation struct {
	ID         FormationID
	Polygons   [][]V
	Min, Max   V
	SectionIDs []int64
	Complete   bool
}

// Contains tests solid rock including its boundary, accounting for holes.
func (f Formation) Contains(p V) bool {
	return (CollisionGeometry{Polygons: f.Polygons}).Contains(p)
}

// GuideGeometry contains a complete guide polyline in world coordinates and
// scene units. S is cumulative arc length along Points. Generation and shading
// parameters are omitted.
type GuideGeometry struct {
	ID       GuideID
	Points   []V
	S        []float64
	Min, Max V
}

// Query finds the nearest rock formation or guide along a finite ray, or at a
// point. It uses exact geometry without camera rounding, hover guide priority,
// collision simplification, or GPU work. At equal distances guides win ties,
// then the older object ID wins. All points use world coordinates, not viewport
// coordinates. Only cached terrain is searched; Query never starts generation.
// Call on the game goroutine after Update, as with other Scene methods.
// Invalid arguments return an error. Closed scenes return an incomplete result.
func (g *Scene) Query(ray Ray, options QueryOptions) (QueryResult, error) {
	ray.Origin = terrain.InternalPoint(g.orientation, ray.Origin)
	ray.Direction = terrain.InternalPoint(g.orientation, ray.Direction)
	result, err := g.query(ray, options)
	if result.Found {
		result.Hit.Point = terrain.WorldPoint(g.orientation, result.Hit.Point)
		result.Hit.Normal = terrain.WorldPoint(g.orientation, result.Hit.Normal)
	}
	return result, err
}

func (g *Scene) query(ray Ray, options QueryOptions) (QueryResult, error) {
	direction, err := validateQuery(ray, options)
	if err != nil {
		return QueryResult{}, err
	}
	if g.closed || g.world == nil {
		return QueryResult{}, nil
	}
	if options.Targets == 0 {
		options.Targets = TargetRock | TargetGuide
	}
	if direction == (V{}) {
		ray.MaxDistance = 0
	}
	index := g.world.queryIndex()
	// The playable world is a unit-wide strip above the floor. Rays may start
	// outside it and enter it; portions outside it are confirmed empty.
	start, end, intersects := geom.RayBox(ray.Origin, direction, ray.MaxDistance,
		V{X: 0, Y: math.Inf(-1)}, V{X: Width, Y: 0})
	if !intersects {
		return QueryResult{Complete: true}, nil
	}
	cutoff, covered := index.coverage(ray.Origin, direction, start, end, options)
	result := QueryResult{Complete: covered}
	limit := end
	if !covered {
		limit = cutoff
	}
	consider := func(hit Hit) {
		// A closing edge at the start of unknown terrain is not a confirmed
		// surface. Never return a hit beyond the first unavailable interval.
		if hit.Distance < start-geom.QueryEpsilon || hit.Distance > limit+geom.QueryEpsilon ||
			(!covered && hit.Distance >= cutoff-geom.QueryEpsilon) {
			return
		}
		if !result.Found || hit.Distance < result.Hit.Distance-geom.QueryEpsilon ||
			(math.Abs(hit.Distance-result.Hit.Distance) <= geom.QueryEpsilon && hitLess(hit, result.Hit)) {
			result.Hit, result.Found, result.Complete = hit, true, true
		}
	}
	if options.Targets&TargetRock != 0 {
		for _, formation := range index.formations {
			if _, _, ok := geom.RayBox(ray.Origin, direction, limit, formation.Min, formation.Max); !ok {
				continue
			}
			hit := Hit{Kind: TargetRock, FormationID: formation.ID}
			if formation.Contains(ray.Origin) {
				hit.Point, hit.StartedInside = ray.Origin, true
				consider(hit)
				continue
			}
			if direction == (V{}) {
				continue
			}
			for _, edge := range index.formationEdges[formation.ID.object] {
				a, b := edge.A, edge.B
				if distance, ok := geom.RaySegment(ray.Origin, direction, a, b, limit); ok {
					hit.Distance = distance
					hit.Point = ray.Origin.Add(direction.Mul(distance))
					hit.Normal = b.Sub(a).Perp().Norm().Mul(-1)
					consider(hit)
				}
			}
		}
	}
	if options.Targets&TargetGuide != 0 {
		radius := options.GuideRadius
		for _, guide := range index.guides {
			if _, _, ok := geom.RayBox(ray.Origin, direction, limit,
				guide.Min.Sub(V{X: radius, Y: radius}), guide.Max.Add(V{X: radius, Y: radius})); !ok {
				continue
			}
			for i, a := range guide.Points[:len(guide.Points)-1] {
				b := guide.Points[i+1]
				distance, normal, inside, ok := geom.RayCapsule(ray.Origin, direction, a, b, radius, limit)
				if ok {
					consider(Hit{Kind: TargetGuide, GuideID: guide.ID,
						Point: ray.Origin.Add(direction.Mul(distance)), Normal: normal,
						Distance: distance, StartedInside: inside})
				}
			}
		}
	}
	return result, nil
}

// Formation fetches an independent copy of a queried formation's cached
// geometry. It returns false for zero, foreign, expired, or unavailable IDs.
// Old IDs of joined formations resolve to the current canonical ID in f.ID.
func (g *Scene) Formation(id FormationID) (f Formation, available bool) {
	if g.closed || g.world == nil || id.object == 0 {
		return Formation{}, false
	}
	index := g.world.queryIndex()
	if id.world != index.worldID {
		return Formation{}, false
	}
	f, available = index.formations[index.aliases[id.object]]
	if available {
		f.Polygons = geom.CopyPolygons(f.Polygons)
		for _, poly := range f.Polygons {
			terrain.MapPoints(poly, func(point geom.V) geom.V {
				return terrain.WorldPoint(g.orientation, point)
			})
		}
		f.Min, f.Max = terrain.WorldBounds(g.orientation, f.Min, f.Max)
		f.SectionIDs = append([]int64(nil), f.SectionIDs...)
	}
	return
}

// Guide fetches an independent copy of a queried guide's geometry. It returns
// false for zero, foreign, expired, or unavailable IDs.
func (g *Scene) Guide(id GuideID) (guide GuideGeometry, available bool) {
	if g.closed || g.world == nil || id.object == 0 {
		return GuideGeometry{}, false
	}
	index := g.world.queryIndex()
	if id.world != index.worldID {
		return GuideGeometry{}, false
	}
	guide, available = index.guides[id.object]
	if available {
		guide.Points = append([]V(nil), guide.Points...)
		terrain.MapPoints(guide.Points, func(point geom.V) geom.V {
			return terrain.WorldPoint(g.orientation, point)
		})
		guide.Min, guide.Max = terrain.WorldBounds(g.orientation, guide.Min, guide.Max)
		guide.S = append([]float64(nil), guide.S...)
	}
	return
}

func validateQuery(ray Ray, options QueryOptions) (V, error) {
	finite := func(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }
	if !finite(ray.Origin.X) || !finite(ray.Origin.Y) || !finite(ray.Direction.X) || !finite(ray.Direction.Y) ||
		!finite(ray.MaxDistance) || ray.MaxDistance < 0 || !finite(options.GuideRadius) || options.GuideRadius < 0 ||
		options.Targets & ^(TargetRock|TargetGuide) != 0 {
		return V{}, fmt.Errorf("infinicave: query requires finite coordinates, nonnegative finite distances, and valid targets")
	}
	scale := math.Max(math.Abs(ray.Direction.X), math.Abs(ray.Direction.Y))
	if scale == 0 || ray.MaxDistance == 0 {
		return V{}, nil
	}
	// Divide components directly so subnormal directions do not
	// overflow the reciprocal of scale.
	direction := V{X: ray.Direction.X / scale, Y: ray.Direction.Y / scale}
	direction = direction.Mul(1 / math.Hypot(direction.X, direction.Y))
	end := ray.Origin.Add(direction.Mul(ray.MaxDistance))
	if !finite(end.X) || !finite(end.Y) {
		return V{}, fmt.Errorf("infinicave: query endpoint overflows world coordinates")
	}
	return direction, nil
}

func hitLess(a, b Hit) bool {
	if a.Kind != b.Kind {
		return a.Kind == TargetGuide
	}
	if a.Kind == TargetGuide {
		return a.GuideID.object < b.GuideID.object
	}
	return a.FormationID.object < b.FormationID.object
}
