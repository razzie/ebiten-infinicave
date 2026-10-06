package infinicave

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"sort"
	"sync/atomic"
)

var queryWorldCounter atomic.Uint64

// Keys and aliases of expired objects are discarded. Counters never reuse IDs.
type worldQueryIndex struct {
	worldID, nextID, revision uint64
	ready                     bool
	faceIDs                   map[[2]int64]uint64
	guideIDs                  map[[32]byte]uint64
	aliases                   map[uint64]uint64
	formations                map[uint64]Formation
	formationEdges            map[uint64][]rockEdge
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
		for _, face := range h.faces {
			faces[face.key] = true
		}
		for _, guide := range h.guides {
			points := make([]V, len(guide.Pts))
			for i, p := range guide.Pts {
				points[i] = p.Add(V{Y: h.top})
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
	geometry *terrainGeometry
	faces    []int
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
		for _, faces := range h.blocks {
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
			key := block.geometry.faces[face].key
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
	formationEdges := make(map[uint64][]rockEdge)
	faceIDs := make(map[[2]int64]uint64)
	aliases := make(map[uint64]uint64)
	for _, r := range roots {
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
				face := h.faces[i]
				keys = append(keys, face.key)
				if old := q.faceIDs[face.key]; old != 0 {
					previous[old] = true
					if id == 0 || old < id {
						id = old
					}
				}
				// Padding discovers connections and missing continuation, but
				// only the owned band contributes geometry to the union.
				lo, hi := face.min.Y+h.top, math.Min(0, face.max.Y+h.top)
				first := max(0, int64(math.Floor(-hi+queryEpsilon)))
				last := int64(math.Ceil(-lo-queryEpsilon)) - 1
				for owner := first; owner <= last; owner++ {
					if s := w.sections[owner]; s == nil || s.geometry == nil {
						complete = false
					}
				}
				poly := clipHalfPlane(face.poly, V{0, -1}, 0)
				poly = clipHalfPlane(poly, V{0, 1}, SectionHeight)
				if len(poly) < 3 || math.Abs(faceArea(poly)) < 1e-15 {
					continue
				}
				worldPoly := make([]V, len(poly))
				for j, p := range poly {
					worldPoly[j] = p.Add(V{Y: h.top})
				}
				grid = append(grid, RockCell{Polygon: worldPoly, Raised: true})
				sectionIDs[-block.section] = true
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
		edges := rockBoundaryEdges(grid, rockNeighbors(grid), true)
		loops, ok := traceCollisionLoops(edges)
		if !ok {
			// Match CollisionGeometry's degenerate-junction fallback. Ray
			// tests still use only union edges, never the internal face edges.
			for _, cell := range grid {
				loops = append(loops, cell.Polygon)
			}
		}
		f.Polygons = loops
		f.Min, f.Max = polygonBounds(grid[0].Polygon)
		for _, cell := range grid[1:] {
			lo, hi := polygonBounds(cell.Polygon)
			f.Min = V{math.Min(f.Min.X, lo.X), math.Min(f.Min.Y, lo.Y)}
			f.Max = V{math.Max(f.Max.X, hi.X), math.Max(f.Max.Y, hi.Y)}
		}
		formations[id], formationEdges[id] = f, edges
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
		for _, g := range h.guides {
			if len(g.Pts) < 2 {
				continue
			}
			points := make([]V, len(g.Pts))
			for i, p := range g.Pts {
				points[i] = p.Add(V{Y: h.top})
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
			guide.Min, guide.Max = polygonBounds(points)
			guideIDs[key], guides[id] = id, guide
		}
	}
	q.faceIDs, q.aliases, q.formations, q.formationEdges = faceIDs, aliases, formations, formationEdges
	q.guideIDs, q.guides = guideIDs, guides
	q.revision, q.ready = w.revision, true
}

func queryGuideKey(points []V) [32]byte {
	bytes := make([]byte, 16*len(points))
	for i, p := range points {
		// Ignore rounding from translating the same padded guide into
		// different section coordinate systems, without relying on Seed
		// (custom loaders may reuse seeds for distinct polylines).
		for axis, value := range []float64{p.X, p.Y} {
			value = math.Round(value*1e7) / 1e7
			if value == 0 {
				value = 0 // canonicalize negative zero
			}
			binary.LittleEndian.PutUint64(bytes[16*i+8*axis:], math.Float64bits(value))
		}
	}
	return sha256.Sum256(bytes)
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
		lo, hi := sectionTop(last)+radius, sectionTop(first)+SectionHeight-radius
		if first == 0 {
			hi = 0 // below the floor is known empty, not unloaded terrain
		}
		if lo > hi {
			continue
		}
		a, b, ok := rayBox(origin, direction, end, V{0, lo}, V{Width, hi})
		if ok {
			intervals = append(intervals, interval{math.Max(start, a), b})
		}
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].start < intervals[j].start })
	cursor := start
	for _, interval := range intervals {
		if interval.end < cursor || interval.start > cursor+queryEpsilon {
			continue
		}
		cursor = math.Max(cursor, interval.end)
		if cursor >= end {
			return end, true
		}
	}
	return cursor, false
}
