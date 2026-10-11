package terrain

import (
	"context"
	"math"
	"sort"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// SpiderWeb spans an acute pocket in the final rock silhouette. Its anchors
// and thread geometry use the same padded section frame as the rock grid.
type SpiderWeb struct {
	Corner, A, B geom.V
	Variation    float64
}

// Frame follows the pocket walls and bows out into the clear opening.
func (w SpiderWeb) Frame() []geom.V {
	points := []geom.V{w.Corner}
	for i := 0; i <= 24; i++ {
		points = append(points, w.Point(float64(i)/24, 1))
	}
	return points
}

// Orb fits a circular capture area within the frame. Its hub lies in open
// space; the rock corner is an attachment, not a fan-shaped hub.
func (w SpiderWeb) Orb() (geom.V, float64) {
	frame := w.Frame()
	axis := w.Point(.5, 1).Sub(w.Corner)
	var hub geom.V
	var radius float64
	for step := 1; step < 32; step++ {
		p := w.Corner.Add(axis.Mul(float64(step) / 32))
		r := math.Inf(1)
		for i, a := range frame {
			b := frame[(i+1)%len(frame)]
			r = math.Min(r, geom.Cross(b.Sub(a), p.Sub(a))/b.Sub(a).Len())
		}
		if r > radius {
			hub, radius = p, r
		}
	}
	return hub, .86 * radius
}

// Point samples the pocket's envelope for clearance during decoration.
// Rendering builds a complete orb inside this envelope.
func (w SpiderWeb) Point(t, radius float64) geom.V {
	a, b := w.A.Sub(w.Corner), w.B.Sub(w.Corner)
	angle := math.Atan2(geom.Cross(a, b), a.Dot(b))
	direction := geom.Rotate(a.Norm(), angle*t)
	chord := b.Sub(a)
	length := geom.Cross(a, b) / geom.Cross(direction, chord)
	tip := direction.Mul(length)
	u := geom.Clamp(tip.Sub(a).Dot(chord)/chord.Len2(), 0, 1)
	depth := geom.Cross(a, b) / chord.Len()
	bow := math.Min(.012, .45*depth) * math.Sin(math.Pi*u)
	return w.Corner.Add(tip.Add(direction.Mul(bow)).Mul(radius))
}

// Candidates follow the final polygon silhouette. Keep every supporting
// face so decoration can reject a pocket with even one illuminated wall.
type webCorner struct {
	web   SpiderWeb
	cells []int
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

func findWebCorners(grid RockGrid, edges []RockEdge) []webCorner {
	return findWebCornersContext(context.Background(), grid, edges)
}

func findWebCornersContext(ctx context.Context, grid RockGrid, edges []RockEdge) []webCorner {
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
	type faceBounds struct{ lo, hi geom.V }
	bounds := make([]faceBounds, len(grid))
	for i, c := range grid {
		if len(c.Polygon) > 0 {
			bounds[i].lo, bounds[i].hi = geom.PolygonBounds(c.Polygon)
		}
	}
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
		random := func(channel int64) float64 {
			return SiteRandom(0x737069646572, worldKey[0], worldKey[1], channel)
		}
		nearby = nearby[:0]
		queried := false
		insideRock := func(p geom.V) bool {
			for _, face := range nearby {
				b := bounds[face]
				if p.X >= b.lo.X && p.X <= b.hi.X && p.Y >= b.lo.Y && p.Y <= b.hi.Y && geom.InsidePolygon(p, grid[face].Polygon) {
					return true
				}
			}
			return false
		}
		var fits []webCorner
		for _, reach := range []float64{.032, .045, .065, .085, .11} {
			w := SpiderWeb{Corner: e.A, Variation: random(3)}
			var left, right []int
			w.A, left = webAnchor(e.A, previous, false, reach*(.9+.2*random(1)), edges, incoming)
			w.B, right = webAnchor(e.A, i, true, reach*(.9+.2*random(2)), edges, outgoing)
			a, b := w.A.Sub(w.Corner), w.B.Sub(w.Corner)
			cosine := a.Norm().Dot(b.Norm())
			// Measure the whole pocket instead of an individual contour join;
			// only acute openings between 20 and 85 degrees support a web.
			if a.Len() < .014 || b.Len() < .014 || geom.Cross(a, b) <= 0 || cosine < math.Cos(85*math.Pi/180) || cosine > math.Cos(20*math.Pi/180) {
				continue
			}
			area := geom.Cross(a, b)
			_, radius := w.Orb()
			if area/b.Sub(a).Len() < .010 || radius < .006 {
				continue
			}
			if !queried {
				// All reaches, anchor jitter, and the bowed frame fit in this
				// square. Select nearby final polygons once per viable pocket,
				// instead of scanning every face for each clearance sample.
				const extent = .11*1.1 + .012 + 1e-9
				for face, b := range bounds {
					if b.hi.X >= e.A.X-extent && b.lo.X <= e.A.X+extent &&
						b.hi.Y >= e.A.Y-extent && b.lo.Y <= e.A.Y+extent {
						nearby = append(nearby, face)
					}
				}
				queried = true
			}
			clear := true
			for spoke := 1; spoke < 12 && clear; spoke++ {
				for ring := 1; ring <= 8; ring++ {
					if insideRock(w.Point(float64(spoke)/12, float64(ring)/8)) {
						clear = false
						break
					}
				}
			}
			if clear {
				fits = append(fits, webCorner{web: w, cells: append(left, right...)})
			}
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

func resolveSpiderWebs(appearance RockGrid, corners []webCorner) []SpiderWeb {
	var webs []SpiderWeb
	for _, c := range corners {
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
		if SiteRandom(0x737069646572, key[0], key[1], 0) > .95 {
			continue
		}
		nearby := false
		for _, other := range webs {
			if w.Corner.Sub(other.Corner).Len() < .055 {
				nearby = true
				break
			}
		}
		if !nearby {
			webs = append(webs, w)
		}
	}
	return webs
}

// PrepareSpiderWebs fits decorations after rock appearance is ready. It returns
// a new snapshot so an already published rock layer never changes underneath it.
func PrepareSpiderWebs(topology *RockTopology) *RockTopology {
	t, _ := prepareSpiderWebsContext(context.Background(), topology)
	return t
}

func prepareSpiderWebsContext(ctx context.Context, topology *RockTopology) (*RockTopology, bool) {
	if ctx.Err() != nil {
		return nil, false
	}
	if topology.websPrepared {
		return topology, true
	}
	t := *PrepareRockAppearance(topology)
	t.webCorners = findWebCornersContext(ctx, t.Grid, t.Boundary)
	if ctx.Err() != nil {
		return nil, false
	}
	t.SpiderWebs = resolveSpiderWebs(t.Appearance, t.webCorners)
	t.websPrepared = true
	return &t, ctx.Err() == nil
}
