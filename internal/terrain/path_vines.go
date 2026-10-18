package terrain

import (
	"context"
	"math"
	"math/rand"
	"sort"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// BranchPath retains the rock skeleton with its original attachment joints.
type BranchPath struct {
	Points        []geom.V
	Widths        []float64
	Parent, Joint int
}

func branchPaths(segments []BranchSegment) []BranchPath {
	type joint struct{ path, point int }
	ends := make(map[[2]int64]joint)
	var paths []BranchPath
	for _, s := range segments {
		if len(paths) > 0 && s.A.Sub(paths[len(paths)-1].Points[len(paths[len(paths)-1].Points)-1]).Len2() < 1e-18 {
			p := &paths[len(paths)-1]
			p.Points = append(p.Points, s.B)
			p.Widths = append(p.Widths, s.WidthB)
		} else {
			p := BranchPath{Points: []geom.V{s.A, s.B}, Widths: []float64{s.WidthA, s.WidthB}, Parent: -1}
			if j, ok := ends[CollisionVertexKey(s.A)]; ok {
				p.Parent, p.Joint = j.path, j.point
			}
			paths = append(paths, p)
		}
		p := &paths[len(paths)-1]
		for i := max(0, len(p.Points)-2); i < len(p.Points); i++ {
			ends[CollisionVertexKey(p.Points[i])] = joint{len(paths) - 1, i}
		}
	}
	return paths
}

type pathSupport struct {
	grid             RockGrid
	faces            *geom.BoundsIndex
	edges, crests    []RockEdge
	boundary, guides *geom.BoundsIndex
	guidePaths       []Guide
	guideClearance   float64
	topCells         RockGrid
	topFaces         *geom.BoundsIndex
	allowedTopCells  []int
}

const vineTopCellClearance = .0015

func vineTopFacing(cell RockCell) bool {
	return len(cell.Polygon) >= 3 && cell.Color.A != 0 && -WorldNormal(cell.orientation, cell.Normal).Y >= .10
}

func edgeBounds(edges []RockEdge, pad float64) []geom.Bounds {
	b := make([]geom.Bounds, len(edges))
	for i, e := range edges {
		b[i] = geom.Bounds{Min: geom.V{X: min(e.A.X, e.B.X) - pad, Y: min(e.A.Y, e.B.Y) - pad}, Max: geom.V{X: max(e.A.X, e.B.X) + pad, Y: max(e.A.Y, e.B.Y) + pad}}
	}
	return b
}

func newPathSupport(topology *RockTopology, guides []Guide) pathSupport {
	s := pathSupport{grid: topology.Grid, edges: topology.Boundary, guidePaths: guides, guideClearance: foregroundVineGuideClearance}
	b := make([]geom.Bounds, len(s.grid))
	for i, c := range s.grid {
		lo, hi := geom.PolygonBounds(c.Polygon)
		b[i] = geom.Bounds{Min: lo, Max: hi}
	}
	s.faces = geom.NewBoundsIndex(b)
	visible := topology.Grid
	if len(topology.Appearance) == len(topology.Grid) {
		visible = topology.Appearance
	}
	var topBounds []geom.Bounds
	for _, c := range visible {
		if !vineTopFacing(c) {
			continue
		}
		s.topCells = append(s.topCells, c)
		lo, hi := geom.PolygonBounds(c.Polygon)
		topBounds = append(topBounds, geom.Bounds{Min: lo, Max: hi})
	}
	s.topFaces = geom.NewBoundsIndex(topBounds)
	s.boundary = geom.NewBoundsIndex(edgeBounds(s.edges, 0))
	for _, g := range guides {
		for j := 1; j < len(g.Pts); j++ {
			s.crests = append(s.crests, RockEdge{A: g.Pts[j-1], B: g.Pts[j]})
		}
	}
	s.guides = geom.NewBoundsIndex(edgeBounds(s.crests, 0))
	return s
}

func (s pathSupport) contains(p geom.V) bool {
	ok := false
	const epsilon = 1e-10
	s.faces.Query(geom.Bounds{Min: p.Sub(geom.V{X: epsilon, Y: epsilon}), Max: p.Add(geom.V{X: epsilon, Y: epsilon})}, func(i int) bool {
		c := s.grid[i]
		if c.Color.A == 0 {
			return true
		}
		ok = geom.InsidePolygon(p, c.Polygon)
		if !ok {
			for j, a := range c.Polygon {
				if geom.SegmentDistanceSquared(p, p, a, c.Polygon[(j+1)%len(c.Polygon)]) < epsilon*epsilon {
					ok = true
					break
				}
			}
		}
		return !ok
	})
	return ok
}

// Validate whole ribbons against exterior boundaries and guide crests. The
// index ignores internal facet joins and catches narrow gaps between samples.
func (s pathSupport) fits(a, b geom.V, radius float64) bool {
	if !s.contains(a) || !s.contains(b) {
		return false
	}
	clear := true
	s.visitTopCells(a, b, radius, func(i int) bool {
		clear = s.topCellAllowed(i)
		return clear
	})
	if !clear {
		return false
	}
	check := func(edges []RockEdge, index *geom.BoundsIndex, pad float64) {
		index.Query(geom.Bounds{Min: geom.V{X: min(a.X, b.X) - pad, Y: min(a.Y, b.Y) - pad}, Max: geom.V{X: max(a.X, b.X) + pad, Y: max(a.Y, b.Y) + pad}}, func(i int) bool {
			e := edges[i]
			if geom.SegmentDistanceSquared(a, b, e.A, e.B) < pad*pad {
				clear = false
			}
			return clear
		})
	}
	// The visual recipe can chip up to .003 units into this smooth boundary.
	check(s.edges, s.boundary, radius+.003)
	if clear {
		check(s.crests, s.guides, radius+s.guideClearance)
	}
	return clear
}

func (s pathSupport) forVine(v Vine) pathSupport {
	s = s.forRoute(v.Route)
	s.allowedTopCells = v.allowedTopCells
	return s
}

func (s pathSupport) forRoute(route VineRoute) pathSupport {
	if route == GuideVineRoute {
		s.guideClearance = guideVineClearance
	}
	return s
}

func foregroundPathVines(ctx context.Context, paths []BranchPath, topology *RockTopology, guides []Guide, seed, id int64) []Vine {
	support := newPathSupport(topology, guides)
	// These paths steer the eventual graph walk; they are not rendered vines.
	// Keep their full extent when they pass through a forbidden top-facing cell.
	proposalSupport := support
	proposalSupport.topCells, proposalSupport.topFaces = nil, nil
	type choice struct {
		path   int
		score  float64
		region [2]int
	}
	var choices []choice
	for i, p := range paths {
		if len(p.Points) < 3 || p.Points[0].Y < 0 || p.Points[0].Y >= SectionHeight {
			continue
		}
		length := 0.0
		for j := 1; j < len(p.Points); j++ {
			length += p.Points[j].Sub(p.Points[j-1]).Len()
		}
		key := CollisionVertexKey(p.Points[0].Add(geom.V{Y: SectionTop(id)}))
		choices = append(choices, choice{path: i, score: length * (.7 + .3*SiteRandom(seed^0x73757266616365, key[0], key[1], 0)), region: [2]int{int(p.Points[0].X / .25), int(p.Points[0].Y / .25)}})
	}
	sort.SliceStable(choices, func(i, j int) bool { return choices[i].score > choices[j].score })
	// Visit the strongest candidate in every occupied region before filling
	// the remaining budget. Failed candidates still get their bounded chance.
	var spread, remaining []choice
	seen := make(map[[2]int]bool)
	for _, c := range choices {
		if !seen[c.region] {
			spread = append(spread, c)
			seen[c.region] = true
		} else {
			remaining = append(remaining, c)
		}
	}
	choices = append(spread, remaining...)
	var vines []Vine
	selectedPaths := make(map[int]int)
	for _, candidate := range choices {
		if ctx.Err() != nil {
			return nil
		}
		if len(vines) >= 12 {
			break
		}
		path := paths[candidate.path]
		for _, side := range []float64{-1, 1} {
			v := Vine{Parent: -1, Foreground: true}
			for j, p := range path.Points {
				a, b := path.Points[max(0, j-1)], path.Points[min(len(path.Points)-1, j+1)]
				t := float64(j) / float64(len(path.Points)-1)
				q := p.Add(b.Sub(a).Norm().Perp().Mul(side*path.Widths[j]*.40 + .002*math.Sin(t*math.Pi*3)))
				r := .0014 * geom.Smoothstep(0, .10, t) * geom.Smoothstep(0, .16, 1-t)
				if q.X < foregroundScreenInset || q.X > 1-foregroundScreenInset || q.Y < GenerationMinY+.02 || q.Y > generationMaxY-.02 {
					break
				}
				prev := q
				if len(v.Points) > 0 {
					prev = v.Points[len(v.Points)-1].P
				}
				if !proposalSupport.fits(prev, q, r) {
					if len(v.Points) < 3 {
						v.Points = nil
						continue
					}
					break
				}
				v.Points = append(v.Points, VinePoint{P: q, Radius: r})
			}
			if length, span := v.extent(); length < .075 || span < .045 {
				continue
			}
			v.Points[0].Radius = 0
			v.Points[len(v.Points)-1].Radius = 0
			crowded := false
			for _, old := range vines {
				if old.Points[0].P.Sub(v.Points[0].P).Len() < .045 {
					crowded = true
					break
				}
			}
			if !crowded {
				if parent, ok := selectedPaths[path.Parent]; ok {
					old := vines[parent]
					joint, nearest := -1, .025*.025
					for j, p := range old.Points {
						if d := p.P.Sub(v.Points[0].P).Len2(); p.Radius > 0 && d < nearest {
							joint, nearest = j, d
						}
					}
					if joint >= 0 && support.fits(old.Points[joint].P, v.Points[0].P, .0014) {
						v.Parent, v.Joint, v.Depth = parent, joint, old.Depth+1
						v.Points = append([]VinePoint{{P: old.Points[joint].P, Radius: old.Points[joint].Radius * .75}}, v.Points...)
					}
				}
				selectedPaths[candidate.path] = len(vines)
				vines = append(vines, v)
				break
			}
		}
	}
	return alignPathVines(topology.Grid, vines, &support, seed^SectionSeed(id, 0x73757266616365))
}

// Background paths prefer the exposed padding, independently of cell colors.
// Each root still owns its complete family across neighboring sections.
func backgroundPathVines(seed, id int64, mode Orientation, footprint *rockFootprint) []Vine {
	if footprint != nil {
		return backgroundPaddingVines(seed, id, footprint)
	}
	rng := rand.New(rand.NewSource(SectionSeed(seed^0x76696e6573, id)))
	var vines []Vine
	families := 7
	for family := 0; family < families; family++ {
		root := geom.V{X: .08 + float64(family)*.17 + (.5-rng.Float64())*.05, Y: .08 + rng.Float64()*.84}
		heading := InternalPoint(mode, geom.V{Y: -1})
		radius := .0042
		if family >= 5 {
			// Sparse runners cross the upright vines at different heights.
			// Roots still own complete paths through the neighboring sections.
			height := .30 + float64(family-5)*.38 + (.5-rng.Float64())*.06
			if mode == Horizontal {
				root.X = height
			} else {
				root = geom.V{X: .08 + .12*rng.Float64(), Y: height}
			}
			heading = InternalPoint(mode, geom.V{X: 1})
			radius = .0034
		}
		phase := rng.Float64() * 2 * math.Pi
		v := freePathInPadding(root, heading, .62+rng.Float64()*.34, radius, phase, -1, 0, footprint)
		if len(v.Points) < 3 {
			continue
		}
		parent := len(vines)
		vines = append(vines, v)
		for fork := 0; fork < 2; fork++ {
			joint := len(v.Points) * (fork + 2) / 5
			if joint >= len(v.Points)-1 || joint < 1 {
				continue
			}
			d := v.Points[joint+1].P.Sub(v.Points[joint-1].P).Norm()
			branch := freePathInPadding(v.Points[joint].P, geom.Rotate(d, float64(2*fork-1)*.7), .18+rng.Float64()*.16, .0026, phase+float64(fork), parent, joint, footprint)
			branch.Depth = 1
			if len(branch.Points) >= 3 {
				vines = append(vines, branch)
			}
		}
	}
	// A few smaller curved twigs supply detail without a polygonal mesh of
	// equally prominent branches. Their ownership remains the original root.
	parents := len(vines)
	for parent := 0; parent < parents; parent++ {
		stem := vines[parent]
		if stem.Parent < 0 || len(stem.Points) < 8 {
			continue
		}
		joint := len(stem.Points) * 3 / 5
		p := stem.Points[joint]
		d := stem.Points[joint+1].P.Sub(stem.Points[joint-1].P).Norm()
		twig := freePathInPadding(p.P, geom.Rotate(d, (rng.Float64()-.5)*1.5), .09+.08*rng.Float64(), p.Radius*.55, rng.Float64()*math.Pi*2, parent, joint, footprint)
		twig.Depth = 2
		if len(twig.Points) > 3 {
			vines = append(vines, twig)
		}
	}
	vines = addFreeVineBranches(vines, nil, rng, footprint)
	decoratePathVines(vines, nil, SectionSeed(seed, id), footprint)
	return vines
}

func freePath(root, heading geom.V, reach, radius, phase float64, parent, joint int) Vine {
	return freePathInPadding(root, heading, reach, radius, phase, parent, joint, nil)
}

// Prefer the middle of the rear band, leaving room for stems and leaves on
// either side. Hidden foreground and unsupported empty space cost more.
func (f *rockFootprint) vinePenalty(p geom.V) float64 {
	cost := 0.0
	if p.X > 0 && p.X < generationWidth {
		band := math.Abs(f.vineDistance(p) - backgroundRockPadding*.48)
		border := min(math.Abs(p.X-.055), math.Abs(p.X-.945))
		cost = min(band, border*.65)
	}
	if rear := f.background; rear != nil {
		// Look ahead near exposed rear edges, so paths turn before their
		// ribbons reach a gap. Distant padding skips containment entirely.
		const reach = .045
		d2 := rear.boundary.Nearest(p, reach*reach, func(i int, best float64) float64 {
			e := rear.edges[i]
			return min(best, geom.SegmentDistanceSquared(p, p, e.A, e.B))
		})
		if d2 < reach*reach {
			d := math.Sqrt(d2)
			if !rear.contains(p) {
				d = -d
			}
			cost += 2 * (reach - d)
		}
	}
	return cost
}

const backgroundVineRockClearance = .018 // includes foreground chips and projected side walls

// Prove the ribbon is outside the foreground's actual bounds. Rear support
// is checked separately: even the outer margins can contain background holes.
func (f *rockFootprint) paddingClearOfForeground(a, b geom.V, radius float64) bool {
	lo, hi := min(a.X, b.X), max(a.X, b.X)
	pad := radius + backgroundVineRockClearance
	return hi <= backgroundSolidBorderWidth-radius-.010 && hi+pad < f.minX || lo >= generationWidth-backgroundSolidBorderWidth+radius+.010 && lo-pad > f.maxX
}

// A supported start and no exposed edge touching the capsule prove the entire
// segment stays on rear rock, including across narrow holes between samples.
func (f *rockFootprint) backgroundSupported(a, b geom.V, margin float64) bool {
	rear := f.background
	if rear == nil {
		return true
	}
	if !rear.contains(a) {
		return false
	}
	clear := true
	rear.boundary.Query(geom.Bounds{Min: geom.V{X: min(a.X, b.X) - margin, Y: min(a.Y, b.Y) - margin}, Max: geom.V{X: max(a.X, b.X) + margin, Y: max(a.Y, b.Y) + margin}}, func(i int) bool {
		e := rear.edges[i]
		clear = geom.SegmentDistanceSquared(a, b, e.A, e.B) > margin*margin
		return clear
	})
	return clear
}

func (f *rockFootprint) paddingSegment(a, b geom.V, radius float64) bool {
	// Keep the direct path away from nearby holes so steering can begin
	// before the next step would touch their rim.
	return f.paddingClearOfForeground(a, b, radius) && f.backgroundSupported(a, b, radius+.045)
}

// Middle runners only need the narrow band beside exposed rock edges. Capping
// the nearest query discards distant faces and rooms. A projection onto an edge
// gives its solid side directly; ambiguous corners use the exact footprint.
func (f *rockFootprint) vineDistance(p geom.V) float64 {
	const reach = backgroundRockPadding + .04
	edge, along := -1, 0.0
	var nearest geom.V
	d2 := f.boundary.Nearest(p, reach*reach, func(i int, best float64) float64 {
		e := f.edges[i]
		d := e.B.Sub(e.A)
		t := geom.Clamp(p.Sub(e.A).Dot(d)/d.Len2(), 0, 1)
		q := e.A.Add(d.Mul(t))
		if distance := p.Sub(q).Len2(); distance < best || distance == best && (edge < 0 || i < edge) {
			edge, along, nearest = i, t, q
			return distance
		}
		return best
	})
	if edge < 0 {
		return math.Inf(1)
	}
	if along < 1e-7 || along > 1-1e-7 {
		return f.distance(p)
	}
	distance := math.Sqrt(d2)
	e := f.edges[edge]
	if geom.Cross(e.B.Sub(e.A), p.Sub(nearest)) > 0 {
		distance = -distance
	}
	return distance
}

// Test the entire ribbon against rock edges, not only growth samples. The
// original uncut silhouette is conservative after authored or runtime carving.
func (f *rockFootprint) backgroundFits(a, b geom.V, radius float64) bool {
	if min(a.X, b.X) < BackgroundMinX+.025 || max(a.X, b.X) > BackgroundMaxX-.025 || min(a.Y, b.Y) < GenerationMinY+.02 || max(a.Y, b.Y) > generationMaxY-.02 {
		return false
	}
	if !f.backgroundSupported(a, b, radius+.010) {
		return false
	}
	if f.paddingClearOfForeground(a, b, radius) {
		return true
	}
	pad := radius + backgroundVineRockClearance
	supported := func(p geom.V) bool {
		d := f.vineDistance(p)
		if math.IsInf(d, 1) && !(p.X+pad < f.minX || p.X-pad > f.maxX) {
			return false
		}
		if f.background != nil {
			return d > pad
		}
		margin := radius + .010
		return d > pad && (p.X <= backgroundSolidBorderWidth-margin || p.X >= generationWidth-backgroundSolidBorderWidth+margin || f.backgroundBorderHeight(p) >= margin || backgroundRockPadding-d >= margin)
	}
	if !supported(a) || a != b && !supported(b) {
		return false
	}
	if a == b {
		return true
	}
	if b.Sub(a).Len2() > .025*.025 {
		if !supported(geom.LerpVector(a, b, .5)) {
			return false
		}
	}
	clear := true
	f.boundary.Query(geom.Bounds{Min: geom.V{X: min(a.X, b.X) - pad, Y: min(a.Y, b.Y) - pad}, Max: geom.V{X: max(a.X, b.X) + pad, Y: max(a.Y, b.Y) + pad}}, func(i int) bool {
		e := f.edges[i]
		clear = geom.SegmentDistanceSquared(a, b, e.A, e.B) > pad*pad
		return clear
	})
	return clear
}

func freePathInPadding(root, heading geom.V, reach, radius, phase float64, parent, joint int, footprint *rockFootprint) Vine {
	v := Vine{Parent: parent, Joint: joint}
	p := root
	previous := heading
	step := .012
	if footprint != nil && footprint.paddingSegment(root, root, radius) {
		step = .020 // Clear margins need fewer samples along their broad curves.
	}
	steps := max(2, int(math.Ceil(reach/step)))
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		if (footprint == nil && (p.X < .02 || p.X > .98)) || p.Y < GenerationMinY+.02 || p.Y > generationMaxY-.02 || (footprint != nil && i == 0 && !footprint.backgroundFits(p, p, radius)) {
			break
		}
		r := radius * math.Pow(1-t, .8)
		if parent < 0 {
			r *= geom.Smoothstep(0, .12, t)
		}
		v.Points = append(v.Points, VinePoint{P: p, Radius: r})
		d := geom.Rotate(heading, .45*math.Sin(t*4+phase)+1.2*geom.Smoothstep(.65, 1, t))
		if footprint != nil {
			// Bounded turns preserve flowing paths while following the padding
			// around bends, branch tips, and overlapping rear formations.
			preferred := previous.Mul(.8).Add(d.Mul(.2)).Norm()
			if next := p.Add(preferred.Mul(reach / float64(steps))); footprint.paddingSegment(p, next, radius) && next.X >= BackgroundMinX+.025 && next.X <= BackgroundMaxX-.025 {
				d, previous = preferred, preferred
				p = next
				continue
			}
			best, score := preferred, math.Inf(1)
			for _, angle := range [...]float64{0, -.28, .28, -.56, .56, -.9, .9, -1.3, 1.3} {
				candidate := geom.Rotate(preferred, angle)
				next := p.Add(candidate.Mul(reach / float64(steps)))
				if !footprint.backgroundFits(p, next, radius) {
					continue
				}
				q := p.Add(candidate.Mul(.028))
				cost := footprint.vinePenalty(q) + .006*math.Abs(angle)
				if cost < score {
					best, score = candidate, cost
				}
			}
			if math.IsInf(score, 1) {
				break
			}
			d, previous = best, best
		}
		p = p.Add(d.Mul(reach / float64(steps)))
	}
	if len(v.Points) > 0 {
		v.Points[len(v.Points)-1].Radius = 0
	}
	return v
}

func decoratePathVines(vines []Vine, support *pathSupport, seed int64, footprint *rockFootprint) {
	for i := range vines {
		v := &vines[i]
		leafSupport := support
		if support != nil {
			growth := support.forVine(*v)
			leafSupport = &growth
		}
		if len(v.Points) == 0 {
			continue
		}
		v.Along = make([]float64, len(v.Points))
		key := CollisionVertexKey(v.Points[0].P)
		rng := rand.New(rand.NewSource(SectionSeed(seed^key[0], key[1])))
		if v.Parent >= 0 {
			v.Style = vines[v.Parent].Style
			v.Along[0] = vines[v.Parent].Along[v.Joint]
		} else {
			h := VineHue(rng.Intn(2))
			v.Style = VineStyle{From: h, To: 1 - h, Start: .03, End: .10, Period: .19, Tint: (rng.Float64() - .5) * .07}
		}
		for j := 1; j < len(v.Points); j++ {
			v.Along[j] = v.Along[j-1] + v.Points[j].P.Sub(v.Points[j-1].P).Len()
		}
		v.styleFamily, v.styleSet = VineFamily(vines, i), true
		nextLeaf := v.Along[0] + .025 + .025*rng.Float64()
		leafSlot := 0
		for j := 1; j < len(v.Points)-1; j++ {
			if v.Along[j] < nextLeaf {
				continue
			}
			nextLeaf = v.Along[j] + .035 + .030*rng.Float64()
			leafSlot++
			p := v.Points[j]
			if p.Radius < .0007 || v.Style.Green(v.Along[j]) < .5 {
				continue
			}
			d := geom.Rotate(v.Points[j+1].P.Sub(v.Points[j-1].P).Norm(), float64(2*(leafSlot%2)-1)*.85)
			length := geom.Clamp(p.Radius*12, .014, .030)
			tip := p.P.Add(d.Mul(length))
			mid := geom.LerpVector(p.P, tip, .45)
			n := d.Perp().Mul(length * .26)
			leaf := VineLeaf{Anchor: p.P, Along: v.Along[j], Polygon: [4]geom.V{p.P, mid.Add(n), tip, mid.Sub(n)}}
			fits := true
			for k, a := range leaf.Polygon {
				b := leaf.Polygon[(k+1)%4]
				if (footprint == nil && (a.X < .02 || a.X > .98)) || a.Y < GenerationMinY+.02 || a.Y > generationMaxY-.02 || (leafSupport != nil && !leafSupport.fits(a, b, .0002)) || (footprint != nil && !footprint.backgroundFits(a, b, .0002)) {
					fits = false
					break
				}
			}
			if fits && leafSupport != nil {
				lo, hi := geom.PolygonBounds(leaf.Polygon[:])
				leafSupport.topFaces.Query(geom.Bounds{Min: lo, Max: hi}, func(i int) bool {
					if !leafSupport.topCellAllowed(i) && geom.InsidePolygon(leafSupport.topCells[i].Polygon[0], leaf.Polygon[:]) {
						fits = false
					}
					return fits
				})
			}
			if fits && footprint != nil && footprint.background != nil {
				// Edge checks alone can miss a small hole enclosed by a leaf.
				rear := footprint.background
				lo, hi := geom.PolygonBounds(leaf.Polygon[:])
				rear.boundary.Query(geom.Bounds{Min: lo, Max: hi}, func(i int) bool {
					if geom.InsidePolygon(rear.edges[i].A, leaf.Polygon[:]) {
						fits = false
					}
					return fits
				})
			}
			if fits {
				v.Leaves = append(v.Leaves, leaf)
			}
		}
	}
}

func surfaceVineLongEnough(v Vine) bool {
	length, span := v.extent()
	return length >= vineMinSurfaceTrunkLength && span >= vineMinSurfaceTrunkSpan
}

// Smoothing can shorten a marginal trunk. Drop its whole family and remap
// surviving attachments before adding guide runners, free twigs, or leaves.
func removeShortSurfaceVines(vines []Vine) []Vine {
	remap := make([]int, len(vines))
	var result []Vine
	for i, v := range vines {
		remap[i] = -1
		if v.Parent < 0 {
			if v.Foreground && !surfaceVineLongEnough(v) {
				continue
			}
		} else {
			if remap[v.Parent] < 0 {
				continue
			}
			v.Parent = remap[v.Parent]
		}
		remap[i] = len(result)
		result = append(result, v)
	}
	return result
}
