package terrain

import (
	"math"
	"math/rand"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

type vineRouteJoint struct{ point, node int }

const (
	vineMinSurfaceTrunkLength = .18
	vineMinSurfaceTrunkSpan   = .10
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
	vines = addFreeVineBranches(vines, support, rng)
	decoratePathVines(vines, support, seed)
	return vines
}

// Center routes start at an actual cell center; seam routes can start anywhere
// on their nearest edge. Both roots stay owned by the source section.
func (r *vineRoutes) trunk(original Vine, rng *rand.Rand, seed int64) (Vine, []vineRouteJoint) {
	key := CollisionVertexKey(original.Points[0].P)
	allowance := vineTopAllowance{}
	if r.support != nil {
		allowance.limit = 2 + int(SiteRandom(seed^0x746f7063656c6c, key[0], key[1], int64(r.route))*2)
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
		if !r.used[i] && q.Y >= 0 && q.Y < SectionHeight && r.fits(q, q, .0015) && distance < limit {
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
			if s := q.Sub(anchor).Norm().Dot(heading); s > score && r.fitsWithTopCells(anchor, q, .0015, allowance) {
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
		radius = .0013
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
