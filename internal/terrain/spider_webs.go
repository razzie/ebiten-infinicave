package terrain

import (
	"context"
	"math"
	"sort"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// DefaultSpiderWebMaxSize is the maximum attachment span in scene units.
const DefaultSpiderWebMaxSize = .15

// DefaultSpiderWebDensity is the eligible-pocket selection probability.
const DefaultSpiderWebDensity = .35

func spiderWebMaxSize(options []float64) float64 {
	if len(options) > 0 && options[0] > 0 {
		return options[0]
	}
	return DefaultSpiderWebMaxSize
}

func spiderWebDensity(options []float64) float64 {
	if len(options) > 1 {
		return options[1]
	}
	return DefaultSpiderWebDensity
}

// SpiderWeb owns a settled silk network. Corner, A, and B identify its search
// pocket; the network can have additional rock attachments beyond those sites.
// All positions use the same padded section frame as the final rock grid.
type SpiderWeb struct {
	Corner, A, B geom.V
	Variation    float64
	Network      *WebNetwork
	supports     []geom.V
}

// Candidates follow the final polygon silhouette. Keep every supporting
// face so decoration can reject a pocket with even one illuminated wall.
type webCorner struct {
	web     SpiderWeb
	anchors []geom.V
	cells   []int
}

// Reduce contour noise only for choosing search vertices. The bounded-error
// simplifier retains real boundary points, including rounded pocket roots;
// anchors, supporting faces, and clearance still use the original contour.
func webSearchVertices(edges []RockEdge, incoming, outgoing map[[2]int64]int) []int {
	visited, candidates := make([]bool, len(edges)), make([]bool, len(edges))
	for seed := range edges {
		if visited[seed] {
			continue
		}
		// Window-clipped contours can be open. Find their start before
		// tracing, so edge sorting cannot split a chain through a pocket.
		first := seed
		for steps := 0; steps < len(edges); steps++ {
			key := CollisionVertexKey(edges[first].A)
			previous, ok := incoming[key]
			if !ok || previous < 0 || outgoing[key] != first || visited[previous] {
				break
			}
			if previous == seed {
				first = seed
				break
			}
			first = previous
		}
		var chain []int
		var points []geom.V
		at := first
		for !visited[at] {
			visited[at] = true
			chain = append(chain, at)
			points = append(points, edges[at].A)
			key := CollisionVertexKey(edges[at].B)
			next, ok := outgoing[key]
			if !ok || next < 0 || incoming[key] != at || visited[next] {
				points = append(points, edges[at].B)
				break
			}
			at = next
		}
		if len(chain) == 0 {
			continue
		}
		last := len(points) - 1
		keep := make([]bool, len(points))
		const tolerance = .0015
		if CollisionVertexKey(points[0]) == CollisionVertexKey(points[last]) {
			// A closed chain needs two distinct endpoints for Douglas-Peucker.
			farthest := 1
			for i := 2; i < last; i++ {
				if points[i].Sub(points[0]).Len2() > points[farthest].Sub(points[0]).Len2() {
					farthest = i
				}
			}
			simplifyCollisionChain(points, 0, farthest, tolerance*tolerance, keep)
			simplifyCollisionChain(points, farthest, last, tolerance*tolerance, keep)
		} else {
			simplifyCollisionChain(points, 0, last, tolerance*tolerance, keep)
		}
		for i, edge := range chain {
			candidates[edge] = keep[i]
		}
	}
	var vertices []int
	for i, candidate := range candidates {
		if candidate {
			vertices = append(vertices, i)
		}
	}
	return vertices
}

func webAnchor(corner geom.V, edge int, forward bool, length float64, edges []RockEdge, links map[[2]int64]int) (geom.V, []int) {
	p := corner
	var cells []int
	// Trace by arc length, not by a fixed number of tiny contour segments.
	// A complete contour is the traversal bound for malformed/short loops.
	for steps := 0; steps < len(edges); steps++ {
		e := edges[edge]
		if len(cells) == 0 || cells[len(cells)-1] != e.Cell {
			cells = append(cells, e.Cell)
		}
		q := e.A
		if forward {
			q = e.B
		}
		d := q.Sub(p)
		if d.Len() >= length {
			return p.Add(d.Norm().Mul(length)), cells
		}
		length -= d.Len()
		p = q
		next, ok := links[CollisionVertexKey(p)]
		if !ok || next < 0 {
			break
		}
		if next == edge || CollisionVertexKey(p) == CollisionVertexKey(corner) {
			break
		}
		edge = next
	}
	return p, cells
}

func findWebCorners(grid RockGrid, edges []RockEdge, maxSize ...float64) []webCorner {
	return findWebCornersContext(context.Background(), grid, edges, maxSize...)
}

func findWebCornersContext(ctx context.Context, grid RockGrid, edges []RockEdge, maxSize ...float64) []webCorner {
	size := spiderWebMaxSize(maxSize)
	reachScale := min(1, size/.18)
	incoming, outgoing := make(map[[2]int64]int), make(map[[2]int64]int)
	add := func(links map[[2]int64]int, p geom.V, index int) {
		key := CollisionVertexKey(p)
		if _, exists := links[key]; exists {
			links[key] = -1
		} else {
			links[key] = index
		}
	}
	for i, e := range edges {
		add(incoming, e.B, i)
		add(outgoing, e.A, i)
	}
	clearance := newWebClearance(grid)
	clearance.edgeIndex = geom.NewBoundsIndex(edgeBounds(edges, 0))
	var corners []webCorner
	var nearby []int
	for _, i := range webSearchVertices(edges, incoming, outgoing) {
		if ctx.Err() != nil {
			return nil
		}
		e := edges[i]
		if !grid[e.Cell].Raised || grid[e.Cell].Color.A == 0 {
			continue
		}
		key := CollisionVertexKey(e.A)
		previous, ok := incoming[key]
		if !ok || previous < 0 || outgoing[key] != i {
			continue
		}
		worldKey := CollisionVertexKey(RockMaterialPoint(grid[e.Cell], e.A))
		if len(maxSize) > 1 && SiteRandom(0x737069646572, worldKey[0], worldKey[1], 0) >= maxSize[1] {
			continue
		}
		random := func(channel int64) float64 {
			return SiteRandom(0x737069646572, worldKey[0], worldKey[1], channel)
		}
		nearby = nearby[:0]
		queried := false
		var fits []webCorner
		for _, reach := range []float64{.045, .065, .09, .12, .16} {
			reach *= reachScale
			w := SpiderWeb{Corner: e.A, Variation: random(3)}
			var left, right []int
			w.A, left = webAnchor(e.A, previous, false, reach*(.65+.60*random(1)), edges, incoming)
			w.B, right = webAnchor(e.A, i, true, reach*(.65+.60*random(2)), edges, outgoing)
			a, b := w.A.Sub(w.Corner), w.B.Sub(w.Corner)
			cosine := a.Norm().Dot(b.Norm())
			// Fit the opening at the scale of the web, including broad recesses.
			if a.Len() < .020 || b.Len() < .020 || geom.Cross(a, b) <= 0 || cosine < math.Cos(140*math.Pi/180) || cosine > math.Cos(20*math.Pi/180) {
				continue
			}
			if geom.Cross(a, b)/b.Sub(a).Len() < .015 {
				continue
			}
			if !queried {
				const extent = .26
				nearby = clearance.nearby(e.A.Sub(geom.V{X: extent, Y: extent}), e.A.Add(geom.V{X: extent, Y: extent}), nearby)
				queried = true
			}
			anchors := []geom.V{w.Corner, w.A, w.B}
			// Shorter attachments along both walls keep the frame suspended
			// away from jagged contours rather than pretending they are silk.
			for _, wall := range []struct {
				edge    int
				forward bool
				length  float64
				links   map[[2]int64]int
			}{
				{previous, false, reach * (.65 + .60*random(1)) * .48, incoming},
				{i, true, reach * (.65 + .60*random(2)) * .56, outgoing},
			} {
				p, _ := webAnchor(e.A, wall.edge, wall.forward, wall.length, edges, wall.links)
				anchors = append(anchors, p)
			}
			center := w.Corner.Add(a.Add(b).Mul(.3))
			clear := true
			for _, p := range anchors {
				if !clearance.segmentClear(center, p, nearby) {
					clear = false
					break
				}
			}
			if !clear {
				continue
			}
			supports := append(append([]int(nil), left...), right...)
			// An exposed lip across the opening can carry an extended frame.
			// Keep the wall-only fit as a fallback if that lip is illuminated
			// or its finished capture threads cannot clear the surrounding rock.
			if p, cell, ok := extraWebAnchor(w, reach, edges, clearance, nearby); ok {
				fits = append(fits, webCorner{web: w, anchors: append(append([]geom.V(nil), anchors...), p), cells: append(append([]int(nil), supports...), cell)})
			}
			fits = append(fits, webCorner{web: w, anchors: anchors, cells: supports})
		}
		// Prefer the largest fit whose entire support is dark. A larger web
		// reaching a pale face must not discard a smaller shaded fit here.
		sort.SliceStable(fits, func(i, j int) bool {
			a, b := fits[i].web, fits[j].web
			return geom.Cross(a.A.Sub(a.Corner), a.B.Sub(a.Corner)) > geom.Cross(b.A.Sub(b.Corner), b.B.Sub(b.Corner))
		})
		corners = append(corners, fits...)
	}
	return corners
}

// All supports must be dark in the same final appearance used by rendering.
// Base growth colors alone miss the pale crest highlights added to top faces.
func darkWebSupport(c RockCell) bool {
	luminance := .30*float64(c.Color.R) + .59*float64(c.Color.G) + .11*float64(c.Color.B)
	return c.Raised && c.Color.A == 255 && luminance <= 55
}

func resolveSpiderWebs(appearance RockGrid, corners []webCorner, options ...float64) []SpiderWeb {
	return resolveSpiderWebsContext(context.Background(), appearance, corners, options...)
}

func resolveSpiderWebsContext(ctx context.Context, appearance RockGrid, corners []webCorner, options ...float64) []SpiderWeb {
	size, density := spiderWebMaxSize(options), spiderWebDensity(options)
	if density <= 0 {
		return nil
	}
	var webs []SpiderWeb
	clearance := newWebClearance(appearance)
	var nearbyFaces []int
	for _, c := range corners {
		if ctx.Err() != nil {
			return nil
		}
		dark := true
		for _, i := range c.cells {
			if !darkWebSupport(appearance[i]) {
				dark = false
				break
			}
		}
		if !dark {
			continue
		}
		w := c.web
		key := CollisionVertexKey(RockMaterialPoint(appearance[c.cells[0]], w.Corner))
		if SiteRandom(0x737069646572, key[0], key[1], 0) >= density {
			continue
		}
		nearby := false
		for _, other := range webs {
			if w.Corner.Sub(other.Corner).Len() < .055 {
				nearby = true
				break
			}
		}
		if nearby {
			continue
		}
		// Test attachment span before settling. All free nodes remain inside
		// the anchors' convex hull, so the cap includes every strand.
		if !webWithinSize(c.anchors, size) {
			continue
		}
		w.Network = buildWebNetwork(c.anchors, w.Variation)
		if w.Network == nil {
			continue
		}
		lo, hi := geom.PolygonBounds(w.Network.Nodes[:w.Network.Anchors])
		nearbyFaces = clearance.nearby(lo, hi, nearbyFaces)
		if !clearance.networkClear(w.Network, nearbyFaces) || webOverlaps(w, webs) {
			continue
		}
		for _, p := range c.anchors {
			for _, cell := range c.cells {
				if q, ok := InsetRockAnchor(appearance[cell].Polygon, p, geom.V{}); ok {
					w.supports = append(w.supports, q)
					break
				}
			}
		}
		if len(w.supports) != len(c.anchors) {
			continue
		}
		webs = append(webs, w)
	}
	return webs
}

// PrepareSpiderWebs fits decorations after rock appearance is ready. It returns
// a new snapshot so an already published rock layer never changes underneath it.
// Optional settings are maximum size followed by eligible-pocket density.
// An omitted or zero maximum size uses DefaultSpiderWebMaxSize; omitted density
// uses DefaultSpiderWebDensity, and explicit zero density disables webs.
func PrepareSpiderWebs(topology *RockTopology, options ...float64) *RockTopology {
	t, _ := prepareSpiderWebsContext(context.Background(), topology, options...)
	return t
}

func prepareSpiderWebsContext(ctx context.Context, topology *RockTopology, options ...float64) (*RockTopology, bool) {
	size, density := spiderWebMaxSize(options), spiderWebDensity(options)
	if ctx.Err() != nil {
		return nil, false
	}
	if topology.websPrepared && topology.webMaxSize == size && topology.webDensity == density {
		return topology, true
	}
	t := *PrepareRockAppearance(topology)
	t.webCorners, t.SpiderWebs = nil, nil
	if density > 0 {
		t.webCorners = findWebCornersContext(ctx, t.Grid, t.Boundary, size, density)
		if ctx.Err() != nil {
			return nil, false
		}
		t.SpiderWebs = resolveSpiderWebsContext(ctx, t.Appearance, t.webCorners, size, density)
	}
	t.websPrepared, t.webMaxSize, t.webDensity = true, size, density
	return &t, ctx.Err() == nil
}

// Maximum pairwise separation is independent of web rotation or scene mode.
func webWithinSize(points []geom.V, size float64) bool {
	for i, a := range points {
		for _, b := range points[i+1:] {
			if a.Sub(b).Len2() > size*size+1e-15 {
				return false
			}
		}
	}
	return true
}

type webFaceBounds struct{ lo, hi geom.V }

type webClearance struct {
	grid      RockGrid
	bounds    []webFaceBounds
	index     *geom.BoundsIndex
	edgeIndex *geom.BoundsIndex
}

func newWebClearance(grid RockGrid) webClearance {
	c := webClearance{grid: grid, bounds: make([]webFaceBounds, len(grid))}
	for i, face := range grid {
		if len(face.Polygon) > 0 {
			c.bounds[i].lo, c.bounds[i].hi = geom.PolygonBounds(face.Polygon)
		}
	}
	bounds := make([]geom.Bounds, len(c.bounds))
	for i, b := range c.bounds {
		bounds[i] = geom.Bounds{Min: b.lo, Max: b.hi}
	}
	c.index = geom.NewBoundsIndex(bounds)
	return c
}

func (c webClearance) nearby(lo, hi geom.V, reuse []int) []int {
	reuse = reuse[:0]
	c.index.Query(geom.Bounds{Min: lo, Max: hi}, func(i int) bool {
		b := c.bounds[i]
		if c.grid[i].Raised && c.grid[i].Color.A != 0 &&
			b.hi.X >= lo.X && b.lo.X <= hi.X && b.hi.Y >= lo.Y && b.lo.Y <= hi.Y {
			reuse = append(reuse, i)
		}
		return true
	})
	return reuse
}

// Check complete segments, not just spaced samples. Trimming only a numerical
// epsilon permits intended rock contacts but still catches thin obstructions.
func (c webClearance) segmentClear(a, b geom.V, faces []int) bool {
	d := b.Sub(a)
	length := d.Len()
	if length < 1e-9 {
		return false
	}
	epsilon := min(1e-8/length, .001)
	a, b = a.Add(d.Mul(epsilon)), b.Sub(d.Mul(epsilon))
	lo := geom.V{X: min(a.X, b.X), Y: min(a.Y, b.Y)}
	hi := geom.V{X: max(a.X, b.X), Y: max(a.Y, b.Y)}
	for _, i := range faces {
		bounds := c.bounds[i]
		if bounds.hi.X < lo.X || bounds.lo.X > hi.X || bounds.hi.Y < lo.Y || bounds.lo.Y > hi.Y {
			continue
		}
		poly := c.grid[i].Polygon
		if geom.InsidePolygon(a, poly) || geom.InsidePolygon(b, poly) || geom.InsidePolygon(geom.LerpVector(a, b, .5), poly) {
			return false
		}
		for j, p := range poly {
			q := poly[(j+1)%len(poly)]
			if max(p.X, q.X) < lo.X || min(p.X, q.X) > hi.X || max(p.Y, q.Y) < lo.Y || min(p.Y, q.Y) > hi.Y {
				continue
			}
			if geom.SegmentDistanceSquared(a, b, p, q) < 1e-22 {
				return false
			}
		}
	}
	return true
}

func (c webClearance) networkClear(n *WebNetwork, faces []int) bool {
	for _, t := range n.Threads {
		if !c.segmentClear(n.Nodes[t.A], n.Nodes[t.B], faces) {
			return false
		}
	}
	return true
}

func extraWebAnchor(w SpiderWeb, reach float64, edges []RockEdge, clearance webClearance, faces []int) (geom.V, int, bool) {
	chord := w.B.Sub(w.A)
	probe := geom.LerpVector(w.A, w.B, .5).Sub(chord.Perp().Norm().Mul(reach * .45))
	center := w.Corner.Add(w.A.Sub(w.Corner).Add(w.B.Sub(w.Corner)).Mul(.3))
	best := reach * reach * .50
	var anchor geom.V
	cell := -1
	var candidates []int
	if clearance.edgeIndex != nil {
		clearance.edgeIndex.Query(geom.Bounds{Min: probe.Sub(geom.V{X: reach, Y: reach}), Max: probe.Add(geom.V{X: reach, Y: reach})}, func(i int) bool { candidates = append(candidates, i); return true })
		sort.Ints(candidates)
	} else {
		for i := range edges {
			candidates = append(candidates, i)
		}
	}
	for _, i := range candidates {
		edge := edges[i]
		d := edge.B.Sub(edge.A)
		if d.Len2() < 1e-20 {
			continue
		}
		// Reject distant edges before projection or polygon queries.
		if max(edge.A.X, edge.B.X) < probe.X-reach || min(edge.A.X, edge.B.X) > probe.X+reach ||
			max(edge.A.Y, edge.B.Y) < probe.Y-reach || min(edge.A.Y, edge.B.Y) > probe.Y+reach {
			continue
		}
		p := edge.A.Add(d.Mul(geom.Clamp(probe.Sub(edge.A).Dot(d)/d.Len2(), .08, .92)))
		if p.Sub(w.Corner).Len() > .25 || geom.Cross(chord, p.Sub(w.A)) >= -reach*reach*.08 {
			continue
		}
		distance := p.Sub(probe).Len2()
		if distance >= best || !clearance.segmentClear(center, p, faces) {
			continue
		}
		best, anchor, cell = distance, p, edge.Cell
	}
	return anchor, cell, cell >= 0
}

// Reject overlapping capture frames, while allowing neighboring attachment
// strands to share a recess without filling the whole opening with silk.
func webOverlaps(w SpiderWeb, others []SpiderWeb) bool {
	frame := webFramePoints(w.Network)
	lo, hi := geom.PolygonBounds(frame)
	for _, other := range others {
		otherFrame := webFramePoints(other.Network)
		olo, ohi := geom.PolygonBounds(otherFrame)
		if hi.X < olo.X || lo.X > ohi.X || hi.Y < olo.Y || lo.Y > ohi.Y {
			continue
		}
		for _, p := range frame {
			if geom.InsidePolygon(p, otherFrame) {
				return true
			}
		}
		for _, p := range otherFrame {
			if geom.InsidePolygon(p, frame) {
				return true
			}
		}
		for i, a := range frame {
			for j, b := range otherFrame {
				if geom.SegmentDistanceSquared(a, frame[(i+1)%len(frame)], b, otherFrame[(j+1)%len(otherFrame)]) < 1e-20 {
					return true
				}
			}
		}
	}
	return false
}

func webFramePoints(n *WebNetwork) []geom.V {
	frame := make([]geom.V, len(n.Frame))
	for i, node := range n.Frame {
		frame[i] = n.Nodes[node]
	}
	return frame
}
