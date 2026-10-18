package terrain

import (
	"math"
	"math/rand"
	"slices"
	"sort"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

type vineRouteJoint struct{ point, node int }

const (
	vineMinSurfaceTrunkLength = .18
	vineMinSurfaceTrunkSpan   = .10
	foregroundVineWidthScale  = 1.5
)

type vineRoutes struct {
	graph   *vineEdgeGraph
	index   *geom.BoundsIndex
	used    []bool
	support *pathSupport
	route   VineRoute
}

func newVineRoutes(grid RockGrid, support *pathSupport) *vineRoutes {
	return newVineRoutesForGraph(newVineEdgeGraph(grid), support, CellEdgeVineRoute)
}

func newVineRoutesForGraph(g *vineEdgeGraph, support *pathSupport, route VineRoute) *vineRoutes {
	bounds := make([]geom.Bounds, len(g.edges))
	for i, e := range g.edges {
		a, b := g.points[e.a], g.points[e.b]
		bounds[i] = geom.Bounds{Min: geom.V{X: min(a.X, b.X), Y: min(a.Y, b.Y)}, Max: geom.V{X: max(a.X, b.X), Y: max(a.Y, b.Y)}}
	}
	return &vineRoutes{graph: g, index: geom.NewBoundsIndex(bounds), used: make([]bool, len(g.edges)), support: support, route: route}
}

func (r *vineRoutes) fits(a, b geom.V, radius float64) bool {
	if a.X < .02 || a.X > .98 || b.X < .02 || b.X > .98 || a.Y < GenerationMinY+.02 || a.Y > generationMaxY-.02 || b.Y < GenerationMinY+.02 || b.Y > generationMaxY-.02 {
		return false
	}
	return r.support == nil || r.support.fits(a, b, radius)
}

// Follow connected seams or cell centers, steered by the broad rock skeleton.
func (r *vineRoutes) trace(root geom.V, edge, end int, guide []VinePoint, heading geom.V, reach, radius float64, rng *rand.Rand, allowances ...vineTopAllowance) (Vine, []vineRouteJoint) {
	allowance := vineTopAllowance{}
	if len(allowances) > 0 {
		allowance = allowances[0]
	}
	v := Vine{Parent: -1, Foreground: r.support != nil, Route: r.route, EdgeAligned: r.route == CellEdgeVineRoute, Points: []VinePoint{{P: root, Radius: radius}}}
	var joints []vineRouteJoint
	path := r.plan(root, edge, end, guide, heading, reach, radius, rng, allowance)
	v.topCellLimit = allowance.limit
	v.allowedTopCells = r.cellsForPath(root, path, radius, allowance)
	length := 0.0
	for _, step := range path {
		p := v.Points[len(v.Points)-1].P
		q := r.graph.points[step.end]
		distance := q.Sub(p).Len()
		count := max(1, int(math.Ceil(distance/.010)))
		for i := 1; i <= count; i++ {
			t := float64(i) / float64(count)
			length += distance / float64(count)
			v.Points = append(v.Points, VinePoint{P: geom.LerpVector(p, q, t), Radius: radius * math.Pow(max(0, 1-length/reach), .7)})
		}
		joints = append(joints, vineRouteJoint{len(v.Points) - 1, step.end})
	}
	if length < .025 {
		return Vine{}, nil
	}
	// Rejected trunk attempts must not consume edges that another root can use.
	if len(guide) > 1 {
		if _, span := v.extent(); length < .06 || span < .035 {
			return Vine{}, nil
		}
		if r.support != nil && !surfaceVineLongEnough(v) {
			return Vine{}, nil
		}
	}
	for _, step := range path {
		r.used[step.edge] = true
	}
	v.Points[len(v.Points)-1].Radius = 0
	return v, joints
}

func alignPathVines(grid RockGrid, source []Vine, support *pathSupport, seed int64) []Vine {
	if support == nil {
		// Only construct the seam graph near this owner's sparse families.
		// The full padded background contains thousands of unrelated facets.
		var edges []RockEdge
		for _, v := range source {
			for j := 1; j < len(v.Points); j++ {
				edges = append(edges, RockEdge{A: v.Points[j-1].P, B: v.Points[j].P})
			}
		}
		index := geom.NewBoundsIndex(edgeBounds(edges, .11))
		var nearby RockGrid
		for _, c := range grid {
			found := false
			index.Query(geom.Bounds{Min: c.Center, Max: c.Center}, func(i int) bool {
				e := edges[i]
				found = geom.SegmentDistanceSquared(c.Center, c.Center, e.A, e.B) < .11*.11
				return !found
			})
			if found {
				nearby = append(nearby, c)
			}
		}
		grid = nearby
	}
	edges := newVineRoutes(grid, support)
	centers := newVineRoutesForGraph(newVineCenterGraph(grid), support, CellCenterVineRoute)
	rng := rand.New(rand.NewSource(seed))
	var vines []Vine
	var joints [][]vineRouteJoint
	var routes []*vineRoutes
	for _, original := range source {
		if original.Parent >= 0 || len(original.Points) < 3 {
			continue
		}
		// Each family can use seams, centers, or both along the same guide.
		selection := rng.Intn(5)
		chosen := []*vineRoutes{edges}
		if selection >= 2 && selection < 4 {
			chosen = []*vineRoutes{centers}
		} else if selection == 4 {
			chosen = []*vineRoutes{edges, centers}
		}
		for _, r := range chosen {
			v, js := r.trunk(original, rng, seed)
			if len(v.Points) == 0 {
				continue
			}
			vines, joints, routes = append(vines, v), append(joints, js), append(routes, r)
		}
	}
	// Fork at real facet junctions, with a second generation on the background.
	depths := 1
	if support == nil {
		depths = 2
	}
	first, last := 0, len(vines)
	for depth := 0; depth < depths; depth++ {
		for parent := first; parent < last; parent++ {
			stem := vines[parent]
			family := parent
			for vines[family].Parent >= 0 {
				family = vines[family].Parent
			}
			r := routes[parent]
			forks := 0
			nextFork := .04 + .04*rng.Float64()
			for j := 1 + rng.Intn(2); j < len(joints[parent])-1 && forks < 3; j++ {
				joint := joints[parent][j]
				along := 0.0
				for k := 1; k <= joint.point; k++ {
					along += stem.Points[k].P.Sub(stem.Points[k-1].P).Len()
				}
				if along < nextFork {
					continue
				}
				nextFork = along + .045 + .045*rng.Float64()
				base := stem.Points[joint.point]
				if base.Radius < .00045 {
					continue
				}
				for _, edge := range r.graph.links[joint.node] {
					if r.used[edge] {
						continue
					}
					e := r.graph.edges[edge]
					end := e.a
					if end == joint.node {
						end = e.b
					}
					heading := r.graph.points[end].Sub(base.P).Norm()
					reach := .045 + rng.Float64()*.080
					if support == nil {
						reach = .10 + rng.Float64()*.17
					}
					allowance := vineTopAllowance{limit: vines[family].topCellLimit, cells: vines[family].allowedTopCells}
					v, js := r.trace(base.P, edge, end, nil, heading, reach, base.Radius*.60, rng, allowance)
					if len(v.Points) < 3 {
						continue
					}
					v.Parent, v.Joint, v.Depth = parent, joint.point, stem.Depth+1
					vines[family].allowedTopCells = v.allowedTopCells
					vines, joints, routes = append(vines, v), append(joints, js), append(routes, r)
					forks++
					break
				}
			}
		}
		first, last = last, len(vines)
	}
	for i := range vines {
		if parent := vines[i].Parent; parent >= 0 {
			vines[i].topCellLimit = vines[parent].topCellLimit
			vines[i].allowedTopCells = vines[parent].allowedTopCells
		}
	}
	pins := make([][]int, len(vines))
	for i, v := range vines {
		if v.Route != CellCenterVineRoute {
			continue
		}
		for _, joint := range joints[i] {
			if joint.node >= routes[i].graph.centerCount {
				pins[i] = append(pins[i], joint.point)
			}
		}
	}
	vines = smoothVineRoutes(vines, support, pins)
	vines = removeShortSurfaceVines(vines)
	vines = addGuideVineBranches(vines, support, rng)
	vines = addFreeVineBranches(vines, support, rng, nil)
	decoratePathVines(vines, support, seed, nil)
	return vines
}

// Center routes start at an actual cell center; seam routes can start anywhere
// on their nearest edge. Both roots stay owned by the source section.
func (r *vineRoutes) trunk(original Vine, rng *rand.Rand, seed int64) (Vine, []vineRouteJoint) {
	key := CollisionVertexKey(original.Points[0].P)
	allowance := vineTopAllowance{}
	if r.support != nil {
		// Let a family traverse a bright ridge rather than spending its entire
		// allowance on the first few facets. Forks share the same cell budget.
		allowance.limit = 6 + int(SiteRandom(seed^0x746f7063656c6c, key[0], key[1], int64(r.route))*4)
	}
	v, joints := r.trunkFrom(original, rng, allowance)
	if len(v.Points) > 0 || r.support == nil {
		return v, joints
	}
	// Retry rejected stubs from the far end and from both directions at an
	// interior start. Use a separate stream so retries do not reshuffle families.
	retry := rand.New(rand.NewSource(SectionSeed(seed^key[0]^int64(r.route), key[1])))
	middle := len(original.Points) / 3
	for _, candidate := range []struct {
		first, last int
	}{
		{len(original.Points) - 1, 0},
		{middle, len(original.Points) - 1},
		{middle, 0},
	} {
		proposal := original
		proposal.Points = nil
		step := 1
		if candidate.first > candidate.last {
			step = -1
		}
		for i := candidate.first; ; i += step {
			proposal.Points = append(proposal.Points, original.Points[i])
			if i == candidate.last {
				break
			}
		}
		if len(proposal.Points) < 3 {
			continue
		}
		if v, joints := r.trunkFrom(proposal, retry, allowance); len(v.Points) > 0 {
			return v, joints
		}
	}
	return Vine{}, nil
}

func (r *vineRoutes) trunkFrom(original Vine, rng *rand.Rand, allowance vineTopAllowance) (Vine, []vineRouteJoint) {
	root := original.Points[0].P
	nearest := .045 * .045
	if r.route == CellCenterVineRoute {
		nearest = .055 * .055
	}
	best := -1
	var anchor geom.V
	r.index.Nearest(root, nearest, func(i int, limit float64) float64 {
		e := r.graph.edges[i]
		a, b := r.graph.points[e.a], r.graph.points[e.b]
		q := a
		if r.route == CellCenterVineRoute {
			if e.a >= r.graph.centerCount {
				q = b
			}
		} else {
			d := b.Sub(a)
			q = a.Add(d.Mul(geom.Clamp(root.Sub(a).Dot(d)/d.Len2(), 0, 1)))
		}
		distance := q.Sub(root).Len2()
		if !r.used[i] && q.Y >= 0 && q.Y < SectionHeight && r.fitsWithTopCells(q, q, .0015*foregroundVineWidthScale, allowance) && distance < limit {
			best, anchor = i, q
			return distance
		}
		return limit
	})
	if best < 0 {
		return Vine{}, nil
	}
	heading := original.Points[min(4, len(original.Points)-1)].P.Sub(root).Norm()
	e := r.graph.edges[best]
	end := e.a
	if r.route == CellCenterVineRoute {
		node := e.a
		if anchor == r.graph.points[e.b] {
			node = e.b
		}
		best = -1
		score := math.Inf(-1)
		for _, candidate := range r.graph.links[node] {
			if r.used[candidate] {
				continue
			}
			e := r.graph.edges[candidate]
			next := e.a
			if next == node {
				next = e.b
			}
			q := r.graph.points[next]
			if s := q.Sub(anchor).Norm().Dot(heading); s > score && r.fitsWithTopCells(anchor, q, .0015*foregroundVineWidthScale, allowance) {
				best, end, score = candidate, next, s
			}
		}
		if best < 0 {
			return Vine{}, nil
		}
	} else if r.graph.points[e.b].Sub(anchor).Dot(heading) > r.graph.points[end].Sub(anchor).Dot(heading) {
		end = e.b
	}
	return r.finishTrunk(original, anchor, best, end, heading, rng, allowance)
}

func (r *vineRoutes) finishTrunk(original Vine, anchor geom.V, edge, end int, heading geom.V, rng *rand.Rand, allowance vineTopAllowance) (Vine, []vineRouteJoint) {
	reach, _ := original.extent()
	reach *= 1.2
	radius := .0032
	if r.support != nil {
		radius = .0013 * foregroundVineWidthScale
		reach = geom.Clamp(reach*1.25, .35, .90)
	}
	v, js := r.trace(anchor, edge, end, original.Points, heading, reach, radius, rng, allowance)
	length, span := v.extent()
	if length < .06 || span < .035 {
		return Vine{}, nil
	}
	along := 0.0
	for i := range v.Points {
		if i > 0 {
			along += v.Points[i].P.Sub(v.Points[i-1].P).Len()
		}
		v.Points[i].Radius *= geom.Smoothstep(0, .035, along)
	}
	return v, js
}

type vineRouteStep struct{ edge, end int }

// Try alternatives at dead ends before committing a stem. A bounded search
// keeps the broad growth direction while finding long paths around top cells.
func (r *vineRoutes) plan(root geom.V, edge, end int, guide []VinePoint, heading geom.V, reach, radius float64, rng *rand.Rand, allowance vineTopAllowance) []vineRouteStep {
	physicalRoutes := *r
	var physical pathSupport
	var topHits [][]int
	var references []int
	distinct := 0
	if r.support != nil && allowance.limit > 0 {
		physical = r.support.withoutTopCells()
		physicalRoutes.support = &physical
		topHits = make([][]int, len(r.graph.edges))
		references = make([]int, len(r.support.topCells))
		for _, cell := range allowance.cells {
			references[cell] = 1
			distinct++
		}
	}
	visited := make([]bool, len(r.graph.points))
	e := r.graph.edges[edge]
	for _, node := range []int{e.a, e.b} {
		if node != end && r.graph.points[node] == root {
			visited[node] = true
		}
	}
	valid := make([]int8, len(r.graph.edges))
	fits := func(edge int) bool {
		if valid[edge] == 0 {
			e := r.graph.edges[edge]
			valid[edge] = -1
			if physicalRoutes.fits(r.graph.points[e.a], r.graph.points[e.b], radius) {
				valid[edge] = 1
				if topHits != nil {
					topHits[edge] = r.support.topCellsTouched(r.graph.points[e.a], r.graph.points[e.b], radius)
				}
			}
		}
		return valid[edge] > 0
	}
	budget := 384
	if len(guide) == 0 {
		budget = 96
	}
	var path, best []vineRouteStep
	longest := 0.0
	var search func(geom.V, int, int, geom.V, float64, int) bool
	search = func(p geom.V, edge, end int, heading geom.V, length float64, target int) bool {
		if budget == 0 || len(path) >= 160 || visited[end] {
			return false
		}
		budget--
		q := r.graph.points[end]
		if len(path) == 0 && !physicalRoutes.fits(p, q, radius) {
			return false
		}
		if topHits != nil {
			hits := topHits[edge]
			if len(path) == 0 {
				hits = r.support.topCellsTouched(p, q, radius)
			}
			added := 0
			for _, cell := range hits {
				if references[cell] == 0 {
					added++
				}
				references[cell]++
			}
			defer func() {
				for _, cell := range hits {
					references[cell]--
				}
			}()
			if distinct+added > allowance.limit {
				return false
			}
			distinct += added
			defer func() { distinct -= added }()
		}
		length += q.Sub(p).Len()
		path = append(path, vineRouteStep{edge, end})
		visited[end] = true
		defer func() {
			visited[end] = false
			path = path[:len(path)-1]
		}()
		if length > longest {
			longest = length
			best = append(best[:0], path...)
		}
		if length >= reach {
			return true
		}
		if d := q.Sub(p); d.Len2() > 1e-18 {
			heading = d.Norm()
		}
		if len(guide) > 1 {
			for target < len(guide)-1 && (q.Sub(guide[target].P).Len() < .035 || q.Sub(guide[target+1].P).Len2() < q.Sub(guide[target].P).Len2()) {
				target++
			}
			d := guide[target].P.Sub(q)
			if target == len(guide)-1 && d.Len() < .05 {
				// Carry on in the guide's final direction instead of curling
				// back toward its endpoint when the requested reach is longer.
				d = guide[target].P.Sub(guide[max(0, target-3)].P)
			}
			if d.Len2() > 1e-8 {
				heading = d.Norm()
			}
		}
		type candidate struct {
			vineRouteStep
			score float64
		}
		var choices []candidate
		for _, next := range r.graph.links[end] {
			if next == edge || r.used[next] || !fits(next) {
				continue
			}
			e := r.graph.edges[next]
			node := e.a
			if node == end {
				node = e.b
			}
			if visited[node] {
				continue
			}
			d := r.graph.points[node].Sub(q)
			score := heading.Dot(d.Norm()) + rng.Float64()*.22
			if topHits != nil {
				for _, cell := range topHits[next] {
					if references[cell] == 0 {
						score -= .15
					}
				}
			}
			if len(guide) > 1 {
				score -= min(.10, r.graph.points[node].Sub(guide[target].P).Len()) * 4
			}
			choices = append(choices, candidate{vineRouteStep{next, node}, score})
		}
		sort.SliceStable(choices, func(i, j int) bool { return choices[i].score > choices[j].score })
		for _, c := range choices {
			if search(q, c.edge, c.end, heading, length, target) {
				return true
			}
		}
		return false
	}
	search(root, edge, end, heading, 0, min(1, len(guide)-1))
	return best
}

type vineTopAllowance struct {
	limit int
	cells []int
}

func (s pathSupport) topCellAllowed(cell int) bool {
	return slices.Contains(s.allowedTopCells, cell)
}

// Count full ribbons against distinct rendered cells, including narrow facets
// between samples and cells whose edges run parallel to a vine seam.
func (s pathSupport) visitTopCells(a, b geom.V, radius float64, visit func(int) bool) {
	visitRockCells(s.topCells, s.topFaces, a, b, radius+vineTopCellClearance, visit)
}

func visitRockCells(cells RockGrid, index *geom.BoundsIndex, a, b geom.V, pad float64, visit func(int) bool) {
	index.Query(geom.Bounds{Min: geom.V{X: min(a.X, b.X) - pad, Y: min(a.Y, b.Y) - pad}, Max: geom.V{X: max(a.X, b.X) + pad, Y: max(a.Y, b.Y) + pad}}, func(i int) bool {
		if len(cells[i].Polygon) < 3 || cells[i].Color.A == 0 {
			return true
		}
		poly := cells[i].Polygon
		if geom.InsidePolygon(a, poly) || geom.InsidePolygon(b, poly) {
			return visit(i)
		}
		for j, p := range poly {
			if geom.SegmentDistanceSquared(a, b, p, poly[(j+1)%len(poly)]) <= pad*pad {
				return visit(i)
			}
		}
		return true
	})
}

func (s pathSupport) topCellsTouched(a, b geom.V, radius float64) []int {
	var cells []int
	s.visitTopCells(a, b, radius, func(i int) bool {
		cells = append(cells, i)
		return true
	})
	return cells
}

func (s pathSupport) withoutTopCells() pathSupport {
	s.topCells, s.topFaces, s.allowedTopCells = nil, nil, nil
	return s
}

func (r *vineRoutes) fitsWithTopCells(a, b geom.V, radius float64, allowance vineTopAllowance) bool {
	if r.support == nil || allowance.limit == 0 {
		return r.fits(a, b, radius)
	}
	physical := r.support.withoutTopCells()
	routes := *r
	routes.support = &physical
	if !routes.fits(a, b, radius) {
		return false
	}
	count := len(allowance.cells)
	r.support.visitTopCells(a, b, radius, func(i int) bool {
		if !slices.Contains(allowance.cells, i) {
			count++
		}
		return count <= allowance.limit
	})
	return count <= allowance.limit
}

func (r *vineRoutes) cellsForPath(root geom.V, path []vineRouteStep, radius float64, allowance vineTopAllowance) []int {
	if r.support == nil || allowance.limit == 0 {
		return nil
	}
	cells := append([]int(nil), allowance.cells...)
	for _, step := range path {
		q := r.graph.points[step.end]
		r.support.visitTopCells(root, q, radius, func(i int) bool {
			if !slices.Contains(cells, i) {
				cells = append(cells, i)
			}
			return true
		})
		root = q
	}
	sort.Ints(cells)
	return cells
}

// Round graph turns within a narrow corridor. Preserve sample identities for
// fork attachments; whole-segment checks keep rounded ribbons on their rock.
func smoothVineRoutes(vines []Vine, support *pathSupport, pins [][]int) []Vine {
	for i := range vines {
		v := &vines[i]
		growth := support
		if support != nil {
			field := support.forVine(*v)
			growth = &field
		}
		fixed := make([]bool, len(v.Points))
		for _, joint := range pins[i] {
			fixed[joint] = true
		}
		v.EdgeAligned = false
		if v.Parent >= 0 {
			v.Points[0] = vines[v.Parent].Points[v.Joint]
			v.Points[0].Radius *= .60
		}
		for sweep := 0; sweep < 3; sweep++ {
			previous := append([]VinePoint(nil), v.Points...)
			for j := 1; j < len(previous)-1; j++ {
				if fixed[j] {
					continue
				}
				a, b, c := previous[j-1], previous[j], previous[j+1]
				target := a.P.Add(c.P).Mul(.25).Add(b.P.Mul(.5))
				d := target.Sub(b.P)
				if d.Len() > .003 {
					target = b.P.Add(d.Norm().Mul(.003))
				}
				radius := math.Max(a.Radius, math.Max(b.Radius, c.Radius))
				if growth != nil && (!growth.fits(v.Points[j-1].P, target, radius) || !growth.fits(target, c.P, radius)) {
					continue
				}
				// A child must be able to reach its rounded parent junction.
				clear := true
				for k := i + 1; k < len(vines); k++ {
					child := vines[k]
					if child.Parent == i && child.Joint == j && len(child.Points) > 1 && growth != nil && !growth.fits(target, child.Points[1].P, radius) {
						clear = false
						break
					}
				}
				if clear {
					v.Points[j].P = target
				}
			}
		}
		old := v.Points
		points := []VinePoint{old[0]}
		remap := make([]int, len(old))
		for j := 1; j < len(old); j++ {
			a, b := old[j-1], old[j]
			c1 := a.P.Add(b.P.Sub(old[max(0, j-2)].P).Mul(1.0 / 6))
			c2 := b.P.Sub(old[min(len(old)-1, j+1)].P.Sub(a.P).Mul(1.0 / 6))
			var span []VinePoint
			if curveSamples([4]geom.V{a.P, c1, c2, b.P}, a.Radius, b.Radius, growth, 0, &span) {
				points = append(points, span...)
			} else {
				points = append(points, b)
			}
			remap[j] = len(points) - 1
		}
		v.Points = points
		for k := i + 1; k < len(vines); k++ {
			if vines[k].Parent == i {
				vines[k].Joint = remap[vines[k].Joint]
			}
		}
		v.Points[len(v.Points)-1].Radius = 0
	}
	return vines
}

// The control hull bounds the cubic's deviation from a chord. Subdivide to
// <= .45 pixels at 1280 pixels/unit and check every emitted ribbon segment.
func curveSamples(p [4]geom.V, r0, r1 float64, support *pathSupport, depth int, out *[]VinePoint) bool {
	const errorBound = .00035
	flat := geom.SegmentDistanceSquared(p[1], p[1], p[0], p[3]) <= errorBound*errorBound && geom.SegmentDistanceSquared(p[2], p[2], p[0], p[3]) <= errorBound*errorBound
	if flat || depth == 8 {
		if support != nil && !support.fits(p[0], p[3], max(r0, r1)) {
			return false
		}
		*out = append(*out, VinePoint{P: p[3], Radius: r1})
		return true
	}
	a, b, c := geom.LerpVector(p[0], p[1], .5), geom.LerpVector(p[1], p[2], .5), geom.LerpVector(p[2], p[3], .5)
	d, e := geom.LerpVector(a, b, .5), geom.LerpVector(b, c, .5)
	m := geom.LerpVector(d, e, .5)
	radius := (r0 + r1) * .5
	return curveSamples([4]geom.V{p[0], a, d, m}, r0, radius, support, depth+1, out) && curveSamples([4]geom.V{m, e, c, p[3]}, radius, r1, support, depth+1, out)
}
