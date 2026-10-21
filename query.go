package infinicave

import (
	"fmt"
	"math"
	"sort"
	"sync/atomic"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// TargetMask selects the objects a world query can hit.
type TargetMask uint8

const (
	TargetRock TargetMask = 1 << iota
	TargetGuide
	TargetBat
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
// remaining parts. Unaffected formation and guide IDs stay valid with Carve,
// CarveCircle, and CarveSegment. CarveTerrain expires all IDs when geometry changes.
type FormationID struct{ world, object uint64 }

// GuideID is an opaque, comparable reference to a guide polyline. It belongs to
// one Scene world and expires after Reset, Close, or eviction of all its copies.
// It also expires when carving completely destroys its supporting formations,
// or when CarveTerrain changes geometry anywhere in the scene.
type GuideID struct{ world, object uint64 }

// BatID is an opaque, comparable reference to a flying bat in one Scene world.
// It expires when the bat is hit, leaves the scene, or on Reset or Close.
// Bat IDs are independent of terrain loading, eviction, and render resolution.
type BatID struct{ world, object uint64 }

// Hit describes a query intersection. Kind is TargetRock, TargetGuide, or
// TargetBat; only the corresponding ID is set. Normal is a unit 2D surface
// normal, pointing out of solid rock or a bat silhouette, or away from a guide's
// radius. An exact guide-line intersection uses a perpendicular facing against
// the ray. Normal is zero for point queries and casts starting inside a target.
// Distance is measured from Ray.Origin.
type Hit struct {
	Kind          TargetMask
	FormationID   FormationID
	GuideID       GuideID
	BatID         BatID
	Point, Normal V
	Distance      float64
	StartedInside bool
}

// QueryResult distinguishes a hit, a confirmed miss, and unloaded terrain.
// Complete means terrain along the ray up to the hit (or the full cast on a
// miss) was available. If unloaded terrain precedes a candidate hit, Found and
// Complete are both false: that candidate cannot be confirmed as nearest.
// Bat-only queries are complete independently of terrain availability.
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

// Query finds the nearest selected rock formation, guide, or flying bat along a
// finite ray, or at a point. It uses exact geometry without camera rounding,
// hover guide priority, collision simplification, or GPU work.
// At equal distances guides win ties, then bats, then rocks; within each kind
// the older object ID wins.
// Bats are opt-in via TargetBat and use their current wing silhouettes.
// All points use world coordinates, not viewport coordinates. Only cached
// terrain is searched; Query never starts generation.
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
	result, cutoff := QueryResult{Complete: true}, math.Inf(1)
	if options.Targets&(TargetRock|TargetGuide) != 0 {
		result, cutoff = g.queryTerrain(ray, direction, options)
	}
	if options.Targets&TargetBat != 0 && g.bats != nil {
		bat, found := g.bats.Raycast(ray.Origin, direction, ray.MaxDistance)
		if found && bat.Distance < cutoff-geom.QueryEpsilon {
			hit := Hit{Kind: TargetBat, BatID: BatID{g.batWorldID(), bat.ID},
				Point: bat.Point, Normal: bat.Normal, Distance: bat.Distance, StartedInside: bat.StartedInside}
			if !result.Found || hit.Distance < result.Hit.Distance-geom.QueryEpsilon ||
				(math.Abs(hit.Distance-result.Hit.Distance) <= geom.QueryEpsilon && hitLess(hit, result.Hit)) {
				result = QueryResult{Hit: hit, Found: true, Complete: true}
			}
		}
	}
	return result, nil
}

// queryTerrain also returns the first unavailable distance, so bat candidates
// cannot bypass terrain that might contain a nearer selected object.
func (g *Scene) queryTerrain(ray Ray, direction V, options QueryOptions) (QueryResult, float64) {
	index := g.world.queryIndex()
	// The playable world is a unit-wide strip above the floor. Rays may start
	// outside it and enter it; portions outside it are confirmed empty.
	start, end, intersects := geom.RayBox(ray.Origin, direction, ray.MaxDistance,
		V{X: 0, Y: math.Inf(-1)}, V{X: Width, Y: 0})
	if !intersects {
		return QueryResult{Complete: true}, math.Inf(1)
	}
	cutoff, covered := index.coverage(ray.Origin, direction, start, end, options)
	result := QueryResult{Complete: covered}
	limit := end
	if !covered {
		limit = cutoff
	} else {
		cutoff = math.Inf(1)
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
	return result, cutoff
}

// HitBat starts the queried bat's fall and burn animation without carving
// terrain. It returns false for zero, foreign, expired, or already-hit IDs.
// Call on the game goroutine, as with Query. Query itself never hits a bat.
func (g *Scene) HitBat(id BatID) bool {
	if g.closed || g.world == nil || g.bats == nil || id.object == 0 || id.world != g.batWorldID() {
		return false
	}
	return g.bats.HitBat(id.object)
}

func (g *Scene) batWorldID() uint64 {
	if g.batQueryWorldID == 0 {
		g.batQueryWorldID = queryWorldCounter.Add(1)
	}
	return g.batQueryWorldID
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
		options.Targets & ^(TargetRock|TargetGuide|TargetBat) != 0 {
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
		return a.Kind == TargetGuide || (a.Kind == TargetBat && b.Kind == TargetRock)
	}
	if a.Kind == TargetGuide {
		return a.GuideID.object < b.GuideID.object
	}
	if a.Kind == TargetBat {
		return a.BatID.object < b.BatID.object
	}
	return a.FormationID.object < b.FormationID.object
}

var queryWorldCounter atomic.Uint64

// Keys and aliases of expired objects are discarded. Counters never reuse IDs.
type worldQueryIndex struct {
	worldID, nextID, revision uint64
	ready                     bool
	faceIDs                   map[[2]int64]uint64
	guideIDs                  map[[32]byte]uint64
	aliases                   map[uint64]uint64
	formations                map[uint64]Formation
	formationEdges            map[uint64][]terrain.RockEdge
	formationBlocks           map[uint64][]queryBlock
	guides                    map[uint64]GuideGeometry
	sections                  []int64 // internal nonnegative IDs, ascending
}

func (w *world) queryIndex() *worldQueryIndex {
	if w.queries == nil {
		w.queries = &worldQueryIndex{worldID: queryWorldCounter.Add(1)}
	}
	if !w.queries.ready || w.queries.revision != w.revision {
		w.queries.rebuild(w)
	}
	return w.queries
}

func (q *worldQueryIndex) allocateID() uint64 {
	q.nextID++
	return q.nextID
}

// Eviction must expire IDs even when no query occurs before a section reloads.
// Otherwise the lazy rebuild could mistake a new cache residency for the old
// one. This drops identity metadata without rebuilding union boundaries.
func (q *worldQueryIndex) expire(w *world) {
	faces := make(map[[2]int64]bool)
	guides := make(map[[32]byte]bool)
	for _, section := range w.sections {
		h := section.geometry
		if h == nil {
			continue
		}
		for _, face := range h.Faces {
			faces[face.Key] = true
		}
		for _, guide := range h.Guides {
			points := make([]V, len(guide.Pts))
			for i, p := range guide.Pts {
				points[i] = p.Add(V{Y: h.Top})
			}
			guides[queryGuideKey(points)] = true
		}
	}
	live := make(map[uint64]bool)
	for key, id := range q.faceIDs {
		if !faces[key] {
			delete(q.faceIDs, key)
		} else {
			live[id] = true
		}
	}
	for id, canonical := range q.aliases {
		if !live[canonical] {
			delete(q.aliases, id)
		}
	}
	for key := range q.guideIDs {
		if !guides[key] {
			delete(q.guideIDs, key)
		}
	}
}

type queryBlock struct {
	section  int64
	geometry *terrain.Geometry
	faces    []int
}

// Match the immutable face slices, rather than geometry pointers: vegetation
// edits and neighboring cuts can replace a section without changing this union.
func sameQueryBlocks(old, current []queryBlock) bool {
	if len(old) != len(current) {
		return false
	}
	for i, b := range current {
		a := old[i]
		if a.section != b.section || len(a.faces) != len(b.faces) {
			return false
		}
		for j, face := range a.faces {
			p, q := a.geometry.Faces[face].Poly, b.geometry.Faces[b.faces[j]].Poly
			if len(p) != len(q) || &p[0] != &q[0] {
				return false
			}
			ah, bh := a.geometry.Faces[face].Holes, b.geometry.Faces[b.faces[j]].Holes
			if len(ah) != len(bh) {
				return false
			}
			for k := range ah {
				if len(ah[k]) != len(bh[k]) || &ah[k][0] != &bh[k][0] {
					return false
				}
			}
		}
	}
	return true
}

func queryFaceComplete(w *world, face terrain.Face, top float64) bool {
	lo, hi := face.Min.Y+top, math.Min(0, face.Max.Y+top)
	first := max(0, int64(math.Floor(-hi+geom.QueryEpsilon)))
	last := int64(math.Ceil(-lo-geom.QueryEpsilon)) - 1
	for owner := first; owner <= last; owner++ {
		if s := w.sections[owner]; s == nil || s.geometry == nil {
			return false
		}
	}
	return true
}

func (q *worldQueryIndex) rebuild(w *world) {
	q.sections = nil
	for id, section := range w.sections {
		if section.geometry != nil {
			q.sections = append(q.sections, id)
		}
	}
	sort.Slice(q.sections, func(i, j int) bool { return q.sections[i] < q.sections[j] })
	var blocks []queryBlock
	for _, id := range q.sections {
		h := w.sections[id].geometry
		for _, faces := range h.Blocks {
			blocks = append(blocks, queryBlock{id, h, faces})
		}
	}
	parents := make([]int, len(blocks))
	for i := range parents {
		parents[i] = i
	}
	var root func(int) int
	root = func(i int) int {
		for parents[i] != i {
			parents[i] = parents[parents[i]]
			i = parents[i]
		}
		return i
	}
	join := func(a, b int) { parents[root(b)] = root(a) }
	byKey := make(map[[2]int64]int)
	byPreviousID := make(map[uint64]int)
	for i, block := range blocks {
		for _, face := range block.faces {
			key := block.geometry.Faces[face].Key
			if other, ok := byKey[key]; ok {
				join(i, other)
			}
			byKey[key] = i
			// Previously connected portions remain one formation if an
			// intermediate section is evicted while both ends stay cached.
			if id := q.faceIDs[key]; id != 0 {
				if other, ok := byPreviousID[id]; ok {
					join(i, other)
				}
				byPreviousID[id] = i
			}
		}
	}
	// Structural components in overlapping windows need not have matching
	// facet keys. Join exact shared area/edges; corner contact stays separate.
	for i, a := range blocks {
		for j := i + 1; j < len(blocks); j++ {
			b := blocks[j]
			if a.section == b.section || a.section-b.section > 2 || b.section-a.section > 2 || root(i) == root(j) {
				continue
			}
		connected:
			for _, ai := range a.faces {
				for _, bi := range b.faces {
					if terrain.ShapesConnect(a.geometry.Shapes[ai], b.geometry.Shapes[bi], V{Y: b.geometry.Top - a.geometry.Top}) {
						join(i, j)
						break connected
					}
				}
			}
		}
	}
	groups := make(map[int][]int)
	var roots []int
	for i := range blocks {
		r := root(i)
		if _, ok := groups[r]; !ok {
			roots = append(roots, r)
		}
		groups[r] = append(groups[r], i)
	}
	formations := make(map[uint64]Formation)
	formationEdges := make(map[uint64][]terrain.RockEdge)
	formationBlocks := make(map[uint64][]queryBlock)
	faceIDs := make(map[[2]int64]uint64)
	aliases := make(map[uint64]uint64)
	for _, r := range roots {
		nodes := groups[r]
		sources := make([]queryBlock, len(nodes))
		for i, node := range nodes {
			sources[i] = blocks[node]
		}
		first := sources[0]
		cachedID := q.faceIDs[first.geometry.Faces[first.faces[0]].Key]
		if cachedID != 0 && sameQueryBlocks(q.formationBlocks[cachedID], sources) {
			f := q.formations[cachedID]
			f.Complete = true
			for _, block := range sources {
				for _, i := range block.faces {
					face := block.geometry.Faces[i]
					faceIDs[face.Key] = cachedID
					f.Complete = f.Complete && queryFaceComplete(w, face, block.geometry.Top)
				}
			}
			formations[cachedID], formationEdges[cachedID] = f, q.formationEdges[cachedID]
			formationBlocks[cachedID], aliases[cachedID] = sources, cachedID
			continue
		}
		id := uint64(0)
		previous := make(map[uint64]bool)
		var keys [][2]int64
		var grid RockGrid
		sectionIDs := make(map[int64]bool)
		complete := true
		for _, node := range groups[r] {
			block := blocks[node]
			h := block.geometry
			for _, i := range block.faces {
				face := h.Faces[i]
				keys = append(keys, face.Key)
				if old := q.faceIDs[face.Key]; old != 0 {
					previous[old] = true
					if id == 0 || old < id {
						id = old
					}
				}
				// Padding discovers connections and missing continuation, but
				// only the owned band contributes geometry to the union.
				complete = complete && queryFaceComplete(w, face, h.Top)
				for _, part := range terrain.ClipShapeBand(h.Shapes[i], 0, SectionHeight) {
					for _, poly := range part.Loops {
						worldPoly := make([]V, len(poly))
						for j, p := range poly {
							worldPoly[j] = p.Add(V{Y: h.Top})
						}
						grid = append(grid, RockCell{Polygon: worldPoly, Raised: true})
						sectionIDs[-block.section] = true
					}
				}
			}
		}
		if len(grid) == 0 {
			continue
		}
		if id == 0 {
			id = q.allocateID()
		}
		for _, key := range keys {
			faceIDs[key] = id
		}
		aliases[id] = id
		for old := range previous {
			aliases[old] = id
		}
		f := Formation{ID: FormationID{q.worldID, id}, Complete: complete}
		for section := range sectionIDs {
			f.SectionIDs = append(f.SectionIDs, section)
		}
		sort.Slice(f.SectionIDs, func(i, j int) bool { return f.SectionIDs[i] > f.SectionIDs[j] })
		edges := terrain.OwnedBoundaryEdges(grid)
		loops, ok := terrain.TraceCollisionLoops(edges)
		if !ok {
			// Match CollisionGeometry's degenerate-junction fallback. Ray
			// tests still use only union edges, never the internal face edges.
			for _, cell := range grid {
				loops = append(loops, cell.Polygon)
			}
		}
		f.Polygons = loops
		f.Min, f.Max = geom.PolygonBounds(grid[0].Polygon)
		for _, cell := range grid[1:] {
			lo, hi := geom.PolygonBounds(cell.Polygon)
			f.Min = V{X: math.Min(f.Min.X, lo.X), Y: math.Min(f.Min.Y, lo.Y)}
			f.Max = V{X: math.Max(f.Max.X, hi.X), Y: math.Max(f.Max.Y, hi.Y)}
		}
		formations[id], formationEdges[id] = f, edges
		formationBlocks[id] = sources
	}
	// Carry forward aliases from earlier merges only while their target is
	// still represented. This also prevents expired IDs from reviving later.
	for old, canonical := range q.aliases {
		if current := aliases[canonical]; current != 0 {
			aliases[old] = current
		}
	}
	guideIDs := make(map[[32]byte]uint64)
	guides := make(map[uint64]GuideGeometry)
	for _, section := range q.sections {
		h := w.sections[section].geometry
		for _, g := range h.Guides {
			if len(g.Pts) < 2 {
				continue
			}
			points := make([]V, len(g.Pts))
			for i, p := range g.Pts {
				points[i] = p.Add(V{Y: h.Top})
			}
			key := queryGuideKey(points)
			if guideIDs[key] != 0 {
				continue
			}
			id := q.guideIDs[key]
			if id == 0 {
				id = q.allocateID()
			}
			guide := GuideGeometry{ID: GuideID{q.worldID, id}, Points: points, S: make([]float64, len(points))}
			for i := 1; i < len(points); i++ {
				guide.S[i] = guide.S[i-1] + points[i].Sub(points[i-1]).Len()
			}
			guide.Min, guide.Max = geom.PolygonBounds(points)
			guideIDs[key], guides[id] = id, guide
		}
	}
	q.faceIDs, q.aliases, q.formations, q.formationEdges = faceIDs, aliases, formations, formationEdges
	q.formationBlocks = formationBlocks
	q.guideIDs, q.guides = guideIDs, guides
	q.revision, q.ready = w.revision, true
}

func queryGuideKey(points []V) [32]byte {
	return terrain.GuideKey(points, 0)
}

// coverage returns the end of the available prefix of the ray's in-world
// interval. Merge neighboring loaded bands before testing, so section seams
// never become gaps. Guide proximity conservatively requires the adjoining
// terrain within GuideRadius in Y as well. Work scales with loaded sections,
// even for rays whose maximum distance spans billions of missing sections.
func (q *worldQueryIndex) coverage(origin, direction V, start, end float64, options QueryOptions) (float64, bool) {
	radius := 0.0
	if options.Targets&TargetGuide != 0 {
		radius = options.GuideRadius
	}
	type interval struct{ start, end float64 }
	var intervals []interval
	for i := 0; i < len(q.sections); {
		first, last := q.sections[i], q.sections[i]
		i++
		for i < len(q.sections) && q.sections[i] == last+1 {
			last = q.sections[i]
			i++
		}
		lo, hi := terrain.SectionTop(last)+radius, terrain.SectionTop(first)+SectionHeight-radius
		if first == 0 {
			hi = 0 // below the floor is known empty, not unloaded terrain
		}
		if lo > hi {
			continue
		}
		a, b, ok := geom.RayBox(origin, direction, end, V{X: 0, Y: lo}, V{X: Width, Y: hi})
		if ok {
			intervals = append(intervals, interval{math.Max(start, a), b})
		}
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].start < intervals[j].start })
	cursor := start
	for _, interval := range intervals {
		if interval.end < cursor || interval.start > cursor+geom.QueryEpsilon {
			continue
		}
		cursor = math.Max(cursor, interval.end)
		if cursor >= end {
			return end, true
		}
	}
	return cursor, false
}
