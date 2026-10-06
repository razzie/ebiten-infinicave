package infinicave

import (
	"fmt"
	"math"
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
	start, end, intersects := rayBox(ray.Origin, direction, ray.MaxDistance,
		V{0, math.Inf(-1)}, V{Width, 0})
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
		if hit.Distance < start-queryEpsilon || hit.Distance > limit+queryEpsilon ||
			(!covered && hit.Distance >= cutoff-queryEpsilon) {
			return
		}
		if !result.Found || hit.Distance < result.Hit.Distance-queryEpsilon ||
			(math.Abs(hit.Distance-result.Hit.Distance) <= queryEpsilon && hitLess(hit, result.Hit)) {
			result.Hit, result.Found, result.Complete = hit, true, true
		}
	}
	if options.Targets&TargetRock != 0 {
		for _, formation := range index.formations {
			if _, _, ok := rayBox(ray.Origin, direction, limit, formation.Min, formation.Max); !ok {
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
				if distance, ok := raySegment(ray.Origin, direction, a, b, limit); ok {
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
			if _, _, ok := rayBox(ray.Origin, direction, limit,
				guide.Min.Sub(V{radius, radius}), guide.Max.Add(V{radius, radius})); !ok {
				continue
			}
			for i, a := range guide.Points[:len(guide.Points)-1] {
				b := guide.Points[i+1]
				distance, normal, inside, ok := rayCapsule(ray.Origin, direction, a, b, radius, limit)
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
		f.Polygons = copyQueryPolygons(f.Polygons)
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
		guide.S = append([]float64(nil), guide.S...)
	}
	return
}

const queryEpsilon = 1e-10

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
	direction := V{ray.Direction.X / scale, ray.Direction.Y / scale}
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

func copyQueryPolygons(polygons [][]V) [][]V {
	copy := make([][]V, len(polygons))
	for i, poly := range polygons {
		copy[i] = append([]V(nil), poly...)
	}
	return copy
}

func rayBox(origin, direction V, distance float64, lo, hi V) (float64, float64, bool) {
	start, end := 0.0, distance
	for _, axis := range [][4]float64{{origin.X, direction.X, lo.X, hi.X}, {origin.Y, direction.Y, lo.Y, hi.Y}} {
		if axis[1] == 0 {
			if axis[0] < axis[2] || axis[0] > axis[3] {
				return 0, 0, false
			}
			continue
		}
		a, b := (axis[2]-axis[0])/axis[1], (axis[3]-axis[0])/axis[1]
		if a > b {
			a, b = b, a
		}
		start, end = math.Max(start, a), math.Min(end, b)
		if start > end {
			return 0, 0, false
		}
	}
	return start, end, true
}

func raySegment(origin, direction, a, b V, limit float64) (float64, bool) {
	edge, offset := b.Sub(a), a.Sub(origin)
	denominator := cross(direction, edge)
	if math.Abs(denominator) <= 1e-14*math.Max(edge.Len(), 1e-12) {
		if math.Abs(cross(offset, direction)) > queryEpsilon {
			return 0, false
		}
		t0, t1 := offset.Dot(direction), b.Sub(origin).Dot(direction)
		start, end := math.Min(t0, t1), math.Max(t0, t1)
		if end < -queryEpsilon || start > limit+queryEpsilon {
			return 0, false
		}
		return math.Max(0, start), true
	}
	distance, fraction := cross(offset, edge)/denominator, cross(offset, direction)/denominator
	if distance < -queryEpsilon || distance > limit+queryEpsilon || fraction < -queryEpsilon || fraction > 1+queryEpsilon {
		return 0, false
	}
	return clamp(distance, 0, limit), true
}

// A guide's selectable area is the union of segment capsules, including round
// end caps. This computes the first entry, not merely the closest approach.
func rayCapsule(origin, direction, a, b V, radius, limit float64) (float64, V, bool, bool) {
	if guideSegmentsDistance2(origin, origin, a, b) <= radius*radius+1e-20 {
		return 0, V{}, true, true
	}
	if direction == (V{}) {
		return 0, V{}, false, false
	}
	if radius == 0 {
		distance, ok := raySegment(origin, direction, a, b, limit)
		normal := b.Sub(a).Perp().Norm()
		if normal.Dot(direction) > 0 {
			normal = normal.Mul(-1)
		}
		return distance, normal, false, ok
	}
	best, normal := math.Inf(1), V{}
	consider := func(distance float64, n V) {
		if distance >= -queryEpsilon && distance <= limit+queryEpsilon && distance < best {
			best, normal = math.Max(0, distance), n
		}
	}
	edge := b.Sub(a)
	if length := edge.Len(); length > 0 {
		tangent := edge.Mul(1 / length)
		perpendicular := tangent.Perp()
		if speed := direction.Dot(perpendicular); speed != 0 {
			for _, sign := range []float64{-1, 1} {
				distance := (sign*radius - origin.Sub(a).Dot(perpendicular)) / speed
				along := origin.Add(direction.Mul(distance)).Sub(a).Dot(tangent)
				if along >= 0 && along <= length {
					consider(distance, perpendicular.Mul(sign))
				}
			}
		}
	}
	for _, center := range []V{a, b} {
		offset := center.Sub(origin)
		projection := offset.Dot(direction)
		perpendicular := cross(offset, direction)
		discriminant := radius*radius - perpendicular*perpendicular
		if discriminant >= 0 {
			distance := projection - math.Sqrt(discriminant)
			n := origin.Add(direction.Mul(distance)).Sub(center).Norm()
			consider(distance, n)
		}
	}
	return best, normal, false, !math.IsInf(best, 1)
}
