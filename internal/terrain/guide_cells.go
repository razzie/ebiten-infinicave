package terrain

import (
	"image/color"
	"math"
	"sort"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// Guides divide the ordinary Voronoi faces along their sampled curves. Each
// resulting face gets its own interior color sample, including a lit fragment
// whose original site lay on the shadow side. No guide sites or rows are added.
func newGuideRockGrid(seeds []geom.V, guides []Guide, colorAt func(geom.V) color.NRGBA) RockGrid {
	grid := guideRockFaces(seeds, guides)
	visible := grid[:0]
	for _, cell := range grid {
		if cell.Color = colorAt(cell.Center); cell.Color.A != 0 {
			visible = append(visible, cell)
		}
	}
	return visible
}

// Retain invisible neighbors until heights and normals have been computed.
func guideRockFaces(seeds []geom.V, guides []Guide) RockGrid {
	cells := voronoiCells(seeds)
	perSite := make([][]guideFragment, len(seeds))
	parallelFor(len(seeds), func(i int) {
		site, poly := seeds[i], cells[i]
		faces := [][]geom.V{poly}
		lo, hi := geom.V{X: generationWidth, Y: generationMaxY}, geom.V{Y: GenerationMinY}
		for _, p := range poly {
			lo.X, lo.Y = math.Min(lo.X, p.X), math.Min(lo.Y, p.Y)
			hi.X, hi.Y = math.Max(hi.X, p.X), math.Max(hi.Y, p.Y)
		}
		for j := range guides {
			g := &guides[j]
			if g.Max.X < lo.X || g.Min.X > hi.X || g.Max.Y < lo.Y || g.Min.Y > hi.Y {
				continue
			}
			var next [][]geom.V
			for _, face := range faces {
				next = append(next, splitGuideFace(face, g)...)
			}
			faces = next
		}
		for _, face := range faces {
			if len(face) < 3 {
				continue
			}
			center := site
			if len(faces) > 1 {
				center = geom.PolygonCenter(face)
			}
			perSite[i] = append(perSite[i], makeMergeGuideFragment(face, center, len(faces) > 1))
		}
	})
	var fragments []guideFragment
	for _, f := range perSite {
		fragments = append(fragments, f...)
	}
	mergeGuideFragments(fragments, guides)
	var grid RockGrid
	for _, fragment := range fragments {
		if len(fragment.poly) < 3 {
			continue
		}
		grid = append(grid, RockCell{Center: fragment.center, Polygon: fragment.poly})
	}
	return grid
}

type guideHit struct {
	path, edge float64 // segment index plus fractional position
	p          geom.V
}

// Cut only paths entering and leaving the face. An open guide tip within a
// face does not divide it, and its tangent is never extended into distant cells.
// Repeating the cut handles curls, multiple crossings and intersecting guides.
func splitGuideFace(poly []geom.V, g *Guide) [][]geom.V {
	var hits []guideHit
	for j := 0; j+1 < len(g.Pts); j++ {
		a, d := g.Pts[j], g.Pts[j+1].Sub(g.Pts[j])
		for i, b := range poly {
			e := poly[(i+1)%len(poly)].Sub(b)
			den := geom.Cross(d, e)
			if math.Abs(den) < 1e-18 {
				continue
			}
			t, u := geom.Cross(b.Sub(a), e)/den, geom.Cross(b.Sub(a), d)/den
			if t >= -1e-9 && t <= 1+1e-9 && u >= -1e-9 && u <= 1+1e-9 {
				t, u = geom.Clamp(t, 0, 1), geom.Clamp(u, 0, 1)
				hits = append(hits, guideHit{float64(j) + t, math.Mod(float64(i)+u, float64(len(poly))), a.Add(d.Mul(t))})
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].path < hits[j].path })
	unique := hits[:0]
	for _, hit := range hits {
		if len(unique) == 0 || hit.p.Sub(unique[len(unique)-1].p).Len2() > 1e-20 {
			unique = append(unique, hit)
		}
	}
	for i := 1; i < len(unique); i++ {
		a, b := unique[i-1], unique[i]
		mid := (a.path + b.path) * .5
		j := int(mid)
		if !geom.InsidePolygon(geom.LerpVector(g.Pts[j], g.Pts[j+1], mid-float64(j)), poly) {
			continue
		}
		path := []geom.V{a.p}
		for k := int(math.Floor(a.path)) + 1; float64(k) < b.path; k++ {
			path = append(path, g.Pts[k])
		}
		path = append(path, b.p)
		left, right := boundaryArc(poly, a, b), boundaryArc(poly, b, a)
		for k := len(path) - 2; k > 0; k-- {
			left = append(left, path[k])
		}
		right = append(right, path[1:len(path)-1]...)
		left, right = geom.CleanPolygon(left), geom.CleanPolygon(right)
		if len(left) < 3 || len(right) < 3 || geom.PolygonArea(left) < 1e-14 || geom.PolygonArea(right) < 1e-14 {
			continue
		}
		return append(splitGuideFace(left, g), splitGuideFace(right, g)...)
	}
	return [][]geom.V{poly}
}

func boundaryArc(poly []geom.V, a, b guideHit) []geom.V {
	end := b.edge
	if end <= a.edge {
		end += float64(len(poly))
	}
	arc := []geom.V{a.p}
	for k := int(math.Floor(a.edge)) + 1; float64(k) < end; k++ {
		arc = append(arc, poly[k%len(poly)])
	}
	return append(arc, b.p)
}

const (
	minGuideFaceArea    = .0001 // about a tenth of an ordinary terrain cell
	minGuideFaceWidth   = .003
	minGuideCompactness = .45 // 1 for a circle; long strips and needles approach 0
	mergeTolerance      = 1e-9
)

type guideFragment struct {
	poly    []geom.V
	center  geom.V
	lo, hi  geom.V
	area    float64
	length  float64
	quality float64
	cut     bool
}

func makeGuideFragment(poly []geom.V, center geom.V, cut bool) guideFragment {
	f := guideFragment{poly: poly, center: center, area: geom.PolygonArea(poly), cut: cut, length: -1, quality: -1,
		lo: geom.V{X: math.Inf(1), Y: math.Inf(1)}, hi: geom.V{X: math.Inf(-1), Y: math.Inf(-1)}}
	for _, p := range poly {
		f.lo.X, f.lo.Y = math.Min(f.lo.X, p.X), math.Min(f.lo.Y, p.Y)
		f.hi.X, f.hi.Y = math.Max(f.hi.X, p.X), math.Max(f.hi.Y, p.Y)
	}
	return f
}

func (f guideFragment) perimeter() float64 {
	if f.length >= 0 {
		return f.length
	}
	length := 0.0
	for i, p := range f.poly {
		length += f.poly[(i+1)%len(f.poly)].Sub(p).Len()
	}
	return length
}

func makeMergeGuideFragment(poly []geom.V, center geom.V, cut bool) guideFragment {
	f := makeGuideFragment(poly, center, cut)
	f.length = f.perimeter()
	f.quality = f.compactness()
	return f
}

// Unlike an absolute width cutoff, compactness catches large, elongated
// fragments too, independent of their orientation or scale.
func (f guideFragment) compactness() float64 {
	if f.quality >= 0 {
		return f.quality
	}
	perimeter := f.perimeter()
	if perimeter == 0 {
		return 0
	}
	return 4 * math.Pi * f.area / (perimeter * perimeter)
}

func (f guideFragment) tiny() bool {
	if len(f.poly) < 3 {
		return false
	}
	return f.compactness() < minGuideCompactness ||
		(f.cut && (f.area < minGuideFaceArea || 2*f.area/f.perimeter() < minGuideFaceWidth))
}

// Merge geometric fragments before assigning color, including invisible faces.
// Keeping both sides here avoids making topology depend on the lighting field.
// Only original cell edges can disappear; a shared guide border is protected.
func mergeGuideFragments(faces []guideFragment, guides []Guide) {
	order := make([]int, len(faces))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		// Ignore roundoff from translating overlapping generation windows.
		return math.Round(faces[order[i]].area*1e11) < math.Round(faces[order[j]].area*1e11)
	})
	index := newFragmentIndex(faces)
	for changed := true; changed; {
		changed = false
		for _, i := range order {
			f := faces[i]
			if !f.tiny() {
				continue
			}
			type neighbor struct {
				index  int
				shared float64
				merged guideFragment
			}
			var neighbors []neighbor
			for _, j := range index.candidates(f) {
				other := faces[j]
				if i == j || len(other.poly) < 3 ||
					other.lo.X > f.hi.X+mergeTolerance || other.hi.X < f.lo.X-mergeTolerance ||
					other.lo.Y > f.hi.Y+mergeTolerance || other.hi.Y < f.lo.Y-mergeTolerance {
					continue
				}
				if shared := mergeableBorder(f.poly, other.poly, guides); shared > mergeTolerance {
					poly := joinFaces(f.poly, other.poly)
					if len(poly) >= 3 {
						merged := makeMergeGuideFragment(poly, geom.PolygonCenter(poly), true)
						// A thin face must become more compact. Merely choosing
						// the best neighbor can still make it thinner, triggering
						// repeated merges into an ever larger sprawling face.
						if f.compactness() < minGuideCompactness &&
							math.Round(merged.compactness()*1e5) <= math.Round(f.compactness()*1e5) {
							continue
						}
						neighbors = append(neighbors, neighbor{j, shared, merged})
					}
				}
			}
			sort.SliceStable(neighbors, func(i, j int) bool {
				a, b := neighbors[i], neighbors[j]
				// Prefer a broad result over extending a sliver end to end.
				if a.merged.tiny() != b.merged.tiny() {
					return !a.merged.tiny()
				}
				qa, qb := math.Round(a.merged.compactness()*1e5), math.Round(b.merged.compactness()*1e5)
				if qa != qb {
					return qa > qb
				}
				return math.Round(a.shared*1e8) > math.Round(b.shared*1e8)
			})
			if len(neighbors) > 0 {
				best := neighbors[0]
				index.remove(best.index, faces[best.index])
				index.remove(i, faces[i])
				faces[best.index] = best.merged
				index.add(best.index, best.merged)
				faces[i].poly = nil
				changed = true
			}
		}
	}
}

// Shared edges may be only partial: a guide can end inside one neighboring
// cell while cutting the other. Measure overlap instead of matching endpoints.
func mergeableBorder(a, b []geom.V, guides []Guide) float64 {
	shared := 0.0
	for i, p := range a {
		d := a[(i+1)%len(a)].Sub(p)
		length := d.Len()
		if length < mergeTolerance {
			continue
		}
		tangent := d.Mul(1 / length)
		for j, q := range b {
			r := b[(j+1)%len(b)]
			if d.Dot(r.Sub(q)) >= 0 || math.Abs(geom.Cross(tangent, q.Sub(p))) > mergeTolerance || math.Abs(geom.Cross(tangent, r.Sub(p))) > mergeTolerance {
				continue
			}
			start, end := math.Max(0, r.Sub(p).Dot(tangent)), math.Min(length, q.Sub(p).Dot(tangent))
			if end-start <= mergeTolerance {
				continue
			}
			mid := p.Add(tangent.Mul((start + end) * .5))
			for k := range guides {
				g := &guides[k]
				if g.distanceBound2(mid) < mergeTolerance*mergeTolerance && g.project(mid).Dist < mergeTolerance {
					return 0
				}
			}
			shared += end - start
		}
	}
	return shared
}

// Union adjacent faces by splitting partial edges and cancelling their shared
// edges. Accept one simple perimeter only; holes and point contacts aren't a
// single rock face and must not be filled by the renderer.
func joinFaces(a, b []geom.V) []geom.V {
	type pointKey [2]int64
	key := func(p geom.V) pointKey { return pointKey{int64(math.Round(p.X * 1e8)), int64(math.Round(p.Y * 1e8))} }
	type edgeKey [2]pointKey
	edges := make(map[edgeKey]bool)
	points := make(map[pointKey]geom.V)
	for side, poly := range [][]geom.V{a, b} {
		other := b
		if side == 1 {
			other = a
		}
		for i, p := range poly {
			d := poly[(i+1)%len(poly)].Sub(p)
			if d.Len2() < mergeTolerance*mergeTolerance {
				continue
			}
			ts := []float64{0, 1}
			for _, q := range other {
				u := q.Sub(p).Dot(d) / d.Len2()
				if u > 0 && u < 1 && q.Sub(p.Add(d.Mul(u))).Len() < mergeTolerance {
					ts = append(ts, u)
				}
			}
			sort.Float64s(ts)
			for j := 1; j < len(ts); j++ {
				x, y := p.Add(d.Mul(ts[j-1])), p.Add(d.Mul(ts[j]))
				kx, ky := key(x), key(y)
				if kx == ky {
					continue
				}
				points[kx], points[ky] = x, y
				if edges[edgeKey{ky, kx}] {
					delete(edges, edgeKey{ky, kx})
				} else if edges[edgeKey{kx, ky}] {
					return nil
				} else {
					edges[edgeKey{kx, ky}] = true
				}
			}
		}
	}
	if len(edges) < 3 {
		return nil
	}
	next := make(map[pointKey]pointKey)
	start := pointKey{math.MaxInt64, math.MaxInt64}
	for edge := range edges {
		if _, exists := next[edge[0]]; exists {
			return nil
		}
		next[edge[0]] = edge[1]
		if edge[0][0] < start[0] || (edge[0][0] == start[0] && edge[0][1] < start[1]) {
			start = edge[0]
		}
	}
	var poly []geom.V
	at := start
	for len(poly) < len(edges) {
		poly = append(poly, points[at])
		var ok bool
		at, ok = next[at]
		if !ok {
			return nil
		}
		if at == start {
			break
		}
	}
	if at != start || len(poly) != len(edges) {
		return nil
	}
	poly = geom.CleanPolygon(poly)
	if math.Abs(geom.PolygonArea(poly)-geom.PolygonArea(a)-geom.PolygonArea(b)) > 1e-11 || len(geom.Triangulate(poly)) != len(poly)-2 {
		return nil
	}
	return poly
}

type fragmentBin [2]int
type fragmentIndex map[fragmentBin][]int

func fragmentBins(f guideFragment, visit func(fragmentBin)) {
	const step = .05
	lo := fragmentBin{int(math.Floor((f.lo.X - mergeTolerance) / step)), int(math.Floor((f.lo.Y - mergeTolerance) / step))}
	hi := fragmentBin{int(math.Floor((f.hi.X + mergeTolerance) / step)), int(math.Floor((f.hi.Y + mergeTolerance) / step))}
	for y := lo[1]; y <= hi[1]; y++ {
		for x := lo[0]; x <= hi[0]; x++ {
			visit(fragmentBin{x, y})
		}
	}
}

func newFragmentIndex(faces []guideFragment) fragmentIndex {
	index := make(fragmentIndex)
	for i, f := range faces {
		index.add(i, f)
	}
	return index
}

func (index fragmentIndex) add(i int, f guideFragment) {
	if len(f.poly) < 3 {
		return
	}
	fragmentBins(f, func(bin fragmentBin) { index[bin] = append(index[bin], i) })
}

func (index fragmentIndex) remove(i int, f guideFragment) {
	fragmentBins(f, func(bin fragmentBin) {
		list := index[bin]
		for j, id := range list {
			if id == i {
				index[bin] = append(list[:j], list[j+1:]...)
				break
			}
		}
	})
}

func (index fragmentIndex) candidates(f guideFragment) []int {
	seen := make(map[int]bool)
	var candidates []int
	fragmentBins(f, func(bin fragmentBin) {
		for _, i := range index[bin] {
			if !seen[i] {
				seen[i] = true
				candidates = append(candidates, i)
			}
		}
	})
	// The merge's stable tie-break remains the original face order.
	sort.Ints(candidates)
	return candidates
}
