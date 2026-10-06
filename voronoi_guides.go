package infinicave

import (
	_ "embed"
	"image/color"
	"math"
	"math/rand"

	"github.com/hajimehoshi/ebiten/v2"
)

const (
	// All generation geometry uses scene units. The owned section is [0,1]²;
	// its generation window adds one unit of padding above and below.
	generationWidth       = Width
	generationMinY        = -SectionHeight
	generationMaxY        = 2 * SectionHeight
	generationHeight      = generationMaxY - generationMinY
	foregroundScreenInset = .018

	guideSpacing   = .020
	guideInfluence = .180
	noiseScale     = 3.4
)

//go:embed grain.kage
var materialShaderSource []byte

//go:embed vines.kage
var vineShaderSource []byte

type V struct{ X, Y float64 }

func (a V) Add(b V) V               { return V{a.X + b.X, a.Y + b.Y} }
func (a V) Sub(b V) V               { return V{a.X - b.X, a.Y - b.Y} }
func (a V) Mul(s float64) V         { return V{a.X * s, a.Y * s} }
func (a V) Dot(b V) float64         { return a.X*b.X + a.Y*b.Y }
func (a V) Len2() float64           { return a.Dot(a) }
func (a V) Len() float64            { return math.Sqrt(a.Len2()) }
func (a V) Perp() V                 { return V{-a.Y, a.X} }
func lerpV(a, b V, t float64) V     { return a.Mul(1 - t).Add(b.Mul(t)) }
func clamp(x, a, b float64) float64 { return max(a, min(b, x)) }
func lerp(a, b, t float64) float64  { return a + (b-a)*t }

func (a V) Norm() V {
	l := a.Len()
	if l == 0 {
		return V{1, 0}
	}
	return a.Mul(1 / l)
}

func smoothstep(a, b, x float64) float64 {
	t := clamp((x-a)/(b-a), 0, 1)
	return t * t * (3 - 2*t)
}

type Guide struct {
	Pts        []V
	S          []float64 // cumulative arc length
	BrightSign float64   // selects a continuous lit side, including around curls
	Seed       int64     // stable random stream for streamed world guides
	Min, Max   V
}

// Each spline segment is a cubic Bezier with matching tangents at its joins.
func cubicBezier(a, b, c, d V, t float64) V {
	u := 1 - t
	return a.Mul(u * u * u).
		Add(b.Mul(3 * u * u * t)).
		Add(c.Mul(3 * u * t * t)).
		Add(d.Mul(t * t * t))
}

// Sample Catmull-Rom curves as Beziers for both drawing and projection.
func splineGuide(knots []V, sign float64) Guide {
	const samplesPerSegment = 64
	pts := make([]V, 1, 1+(len(knots)-1)*samplesPerSegment)
	pts[0] = knots[0]
	for i := 0; i < len(knots)-1; i++ {
		a, d := knots[i], knots[i+1]
		prev, next := a.Mul(2).Sub(d), d.Mul(2).Sub(a)
		if i > 0 {
			prev = knots[i-1]
		}
		if i+2 < len(knots) {
			next = knots[i+2]
		}
		b := a.Add(d.Sub(prev).Mul(1.0 / 6))
		c := d.Sub(next.Sub(a).Mul(1.0 / 6))
		for j := 1; j <= samplesPerSegment; j++ {
			pts = append(pts, cubicBezier(a, b, c, d, float64(j)/samplesPerSegment))
		}
	}
	ss := make([]float64, len(pts))
	for i := 1; i < len(pts); i++ {
		ss[i] = ss[i-1] + pts[i].Sub(pts[i-1]).Len()
	}
	lo, hi := pts[0], pts[0]
	for _, p := range pts {
		lo.X = math.Min(lo.X, p.X)
		lo.Y = math.Min(lo.Y, p.Y)
		hi.X = math.Max(hi.X, p.X)
		hi.Y = math.Max(hi.Y, p.Y)
	}
	return Guide{Pts: pts, S: ss, BrightSign: sign, Min: lo, Max: hi}
}

type Projection struct {
	Q      V
	T, N   V
	S      float64
	Signed float64
	Dist   float64
}

func (g *Guide) translateY(offset float64) {
	for i := range g.Pts {
		g.Pts[i].Y += offset
	}
	g.Min.Y += offset
	g.Max.Y += offset
}

func (g *Guide) distanceBound2(p V) float64 {
	dx := max(0, g.Min.X-p.X, p.X-g.Max.X)
	dy := max(0, g.Min.Y-p.Y, p.Y-g.Max.Y)
	return dx*dx + dy*dy
}

func (g *Guide) project(p V) Projection {
	best2 := math.Inf(1)
	segment, u := 0, 0.0
	var q V
	for i := 0; i < len(g.Pts)-1; i++ {
		a, b := g.Pts[i], g.Pts[i+1]
		d := b.Sub(a)
		l2 := d.Len2()
		if l2 == 0 {
			continue
		}
		t := clamp(p.Sub(a).Dot(d)/l2, 0, 1)
		point := a.Add(d.Mul(t))
		distance2 := p.Sub(point).Len2()
		if distance2 < best2 {
			best2, segment, u, q = distance2, i, t, point
		}
	}
	tangent := g.segmentTangent(segment, u)
	normal := tangent.Perp().Mul(g.BrightSign)
	return Projection{Q: q, T: tangent, N: normal, S: lerp(g.S[segment], g.S[segment+1], u), Signed: p.Sub(q).Dot(normal), Dist: math.Sqrt(best2)}
}

// Interpolate vertex tangents so lighting and branches stay smooth at sample boundaries.
func (g *Guide) segmentTangent(i int, u float64) V {
	t := g.Pts[i+1].Sub(g.Pts[i]).Norm()
	a, b := t, t
	if i > 0 {
		a = g.Pts[i].Sub(g.Pts[i-1]).Norm().Add(t).Norm()
	}
	if i+2 < len(g.Pts) {
		b = t.Add(g.Pts[i+2].Sub(g.Pts[i+1]).Norm()).Norm()
	}
	return lerpV(a, b, u).Norm()
}

func (g *Guide) frameAt(s float64) (q, t, n V) {
	s = clamp(s, 0, g.S[len(g.S)-1])
	i := 0
	for i+1 < len(g.S)-1 && g.S[i+1] < s {
		i++
	}
	a, b := g.Pts[i], g.Pts[i+1]
	seg := g.S[i+1] - g.S[i]
	u := 0.0
	if seg > 0 {
		u = (s - g.S[i]) / seg
	}
	q = lerpV(a, b, u)
	t = g.segmentTangent(i, u)
	n = t.Perp().Mul(g.BrightSign)
	return
}

func nearestGuide(p V, guides []Guide) (int, Projection) {
	bestI := -1
	best := Projection{Dist: math.Inf(1)}
	for i := range guides {
		if guides[i].distanceBound2(p) >= best.Dist*best.Dist {
			continue
		}
		pr := guides[i].project(p)
		if pr.Dist < best.Dist {
			bestI, best = i, pr
		}
	}
	return bestI, best
}

// Site density follows terrain only; guides cut faces without adding sites.
func desiredSpacing(p V, noise *Perlin) float64 {
	return lerp(.030, .022, smoothstep(.39, .58, fbm(noise, p)))
}

func generateSeeds(rng *rand.Rand, count int, noise *Perlin) []V {
	// Weighted sampling follows background terrain.
	pts := make([]V, 0, count)
	for len(pts) < count {
		best := V{}
		bestD2 := -1.0
		candidates := 22
		if len(pts) < 8 {
			candidates = 8
		}
		for c := 0; c < candidates; c++ {
			p := V{rng.Float64() * generationWidth, generationMinY + rng.Float64()*generationHeight}
			minD2 := math.Inf(1)
			for _, q := range pts {
				d2 := p.Sub(q).Len2()
				if d2 < minD2 {
					minD2 = d2
				}
			}
			spacing := desiredSpacing(p, noise)
			minD2 /= spacing * spacing
			if minD2 > bestD2 {
				best, bestD2 = p, minD2
			}
		}
		pts = append(pts, best)
	}
	return pts
}

// Repel crowded seeds without forcing a regular lattice.
func relaxSeeds(seeds []V, noise *Perlin) {
	spacing := make([]float64, len(seeds))
	for i, p := range seeds {
		spacing[i] = desiredSpacing(p, noise) * .48
	}
	for pass := 0; pass < 4; pass++ {
		shifts := make([]V, len(seeds))
		for i, p := range seeds {
			for j := i + 1; j < len(seeds); j++ {
				d := p.Sub(seeds[j])
				distance := d.Len()
				minimum := (spacing[i] + spacing[j]) * .5
				if distance >= minimum {
					continue
				}
				push := d.Norm().Mul((minimum - distance) * .35)
				shifts[i] = shifts[i].Add(push)
				shifts[j] = shifts[j].Sub(push)
			}
		}
		for i := range seeds {
			seeds[i] = seeds[i].Add(shifts[i])
			seeds[i].X = clamp(seeds[i].X, .001, generationWidth-.001)
			seeds[i].Y = clamp(seeds[i].Y, generationMinY+.001, generationMaxY-.001)
		}
	}
}

// Classic gradient Perlin noise.
type Perlin struct {
	p       [512]int
	OffsetY float64
}

func NewPerlin(rng *rand.Rand) *Perlin {
	q := rng.Perm(256)
	n := &Perlin{}
	for i := 0; i < 512; i++ {
		n.p[i] = q[i&255]
	}
	return n
}

func fade(t float64) float64 { return t * t * t * (t*(t*6-15) + 10) }

func grad(h int, x, y float64) float64 {
	switch h & 7 {
	case 0:
		return x + y
	case 1:
		return -x + y
	case 2:
		return x - y
	case 3:
		return -x - y
	case 4:
		return x
	case 5:
		return -x
	case 6:
		return y
	default:
		return -y
	}
}

func (n *Perlin) Noise(x, y float64) float64 {
	x0 := math.Floor(x)
	y0 := math.Floor(y)
	xi := int(x0) & 255
	yi := int(y0) & 255
	xf := x - x0
	yf := y - y0
	u, v := fade(xf), fade(yf)

	aa := n.p[n.p[xi]+yi]
	ab := n.p[n.p[xi]+yi+1]
	ba := n.p[n.p[xi+1]+yi]
	bb := n.p[n.p[xi+1]+yi+1]

	x1 := lerp(grad(aa, xf, yf), grad(ba, xf-1, yf), u)
	x2 := lerp(grad(ab, xf, yf-1), grad(bb, xf-1, yf-1), u)
	return lerp(x1, x2, v) * 0.7071
}

func fbm(n *Perlin, p V) float64 {
	p.Y += n.OffsetY
	a, f, sum, norm := 1.0, noiseScale, 0.0, 0.0
	for i := 0; i < 4; i++ {
		sum += a * n.Noise(p.X*f, p.Y*f)
		norm += a
		a *= 0.52
		f *= 2.08
	}
	return 0.5 + 0.5*sum/norm
}

func cellColor(v float64) color.NRGBA {
	// Sampled rock colors stay charcoal, with restrained warm-gray highlights.
	stops := []struct {
		value   float64
		r, g, b float64
	}{
		{0, 0, 0, 0},
		{.10, 8, 8, 7},
		{.25, 18, 18, 16},
		{.43, 38, 37, 34},
		{.65, 120, 116, 106},
		{.85, 165, 158, 142},
		{1, 190, 182, 164},
	}
	v = clamp(v, 0, 1)
	for i := 1; i < len(stops); i++ {
		a, b := stops[i-1], stops[i]
		if v <= b.value {
			t := (v - a.value) / (b.value - a.value)
			return color.NRGBA{uint8(math.Round(lerp(a.r, b.r, t))), uint8(math.Round(lerp(a.g, b.g, t))), uint8(math.Round(lerp(a.b, b.b, t))), 255}
		}
	}
	return color.NRGBA{221, 215, 200, 255}
}

// The underlying tessellation has no guide influence. Low Perlin values map
// to exact black; the remaining cells stay within the charcoal palette.
func backgroundCellColor(p V, noise *Perlin) color.NRGBA {
	return backgroundSurfaceColor(p, noise, V3{Z: 1})
}

func backgroundSurfaceColor(p V, noise *Perlin, normal V3) color.NRGBA {
	return cellColor(.38 * smoothstep(.43, .59, fbm(noise, p)) * (.28 + .72*surfaceLight(normal)))
}

// Rock occupancy follows relief, independently of light. Every existing face
// is opaque, including its dark flank and the tapered ends of raised spurs.
func guideCellColor(p V, guides []Guide, noise *Perlin, branches *BranchField) color.NRGBA {
	dx := reliefHeight(p.Add(V{.002, 0}), guides, noise, branches) - reliefHeight(p.Sub(V{.002, 0}), guides, noise, branches)
	dy := reliefHeight(p.Add(V{0, .002}), guides, noise, branches) - reliefHeight(p.Sub(V{0, .002}), guides, noise, branches)
	return guideSurfaceColor(p, guides, noise, branches, (V3{-dx / .004, -dy / .004, 1}).Norm())
}

func guideSurfaceColor(p V, guides []Guide, noise *Perlin, branches *BranchField, normal V3) color.NRGBA {
	if reliefHeight(p, guides, noise, branches) <= rockContourHeight {
		return color.NRGBA{}
	}
	return rockSurfaceColor(normal, 1, 1)
}

func clipHalfPlane(poly []V, n V, c float64) []V {
	if len(poly) == 0 {
		return poly
	}
	// Most sites cannot cut an already small cell. Return the existing
	// polygon unchanged instead of allocating for every distant half-plane.
	inside := 0
	for _, p := range poly {
		if p.Dot(n) <= c+1e-15 {
			inside++
		}
	}
	if inside == len(poly) {
		return poly
	}
	if inside == 0 {
		return nil
	}
	out := make([]V, 0, len(poly)+1)
	prev := poly[len(poly)-1]
	prevIn := prev.Dot(n) <= c+1e-15
	for _, cur := range poly {
		curIn := cur.Dot(n) <= c+1e-15
		if curIn != prevIn {
			d := cur.Sub(prev)
			den := d.Dot(n)
			if math.Abs(den) > 1e-18 {
				t := (c - prev.Dot(n)) / den
				out = append(out, prev.Add(d.Mul(t)))
			}
		}
		if curIn {
			out = append(out, cur)
		}
		prev, prevIn = cur, curIn
	}
	return out
}

func voronoiCell(i int, pts []V) []V {
	poly := []V{{0, generationMinY}, {generationWidth, generationMinY}, {generationWidth, generationMaxY}, {0, generationMaxY}}
	a := pts[i]
	for j, b := range pts {
		if i == j {
			continue
		}
		n := b.Sub(a)
		c := 0.5 * (b.Len2() - a.Len2())
		poly = clipHalfPlane(poly, n, c)
		if len(poly) == 0 {
			break
		}
	}
	return orderPolygon(poly)
}

func orderPolygon(poly []V) []V {
	if len(poly) > 0 {
		first := 0
		for j := 1; j < len(poly); j++ {
			ax, bx := math.Round(poly[j].X*1e9), math.Round(poly[first].X*1e9)
			if ax < bx || (ax == bx && poly[j].Y < poly[first].Y) {
				first = j
			}
		}
		ordered := append([]V{}, poly[first:]...)
		poly = append(ordered, poly[:first]...)
	}
	return poly
}

// Faces visible from their center use a triangle fan. Deep concave guide
// cuts use ear clipping, with material rims only on the actual perimeter.
func appendCellMesh(vertices []ebiten.Vertex, indices []uint32, poly []V, center V, clr color.NRGBA, surface V3) ([]ebiten.Vertex, []uint32) {
	if len(poly) < 3 {
		return vertices, indices
	}
	radius := 0.0
	for _, p := range poly {
		radius = math.Max(radius, p.Sub(center).Len())
	}
	radius = math.Max(radius, .001)
	tilt := V{surface.X, surface.Y}
	type facetTriangle struct {
		center, a, b V
		boundary     bool
	}
	var facets []facetTriangle
	fan := true
	for i, a := range poly {
		if cross(poly[(i+1)%len(poly)].Sub(a), center.Sub(a)) < -1e-15 {
			fan = false
			break
		}
	}
	if fan {
		for i, a := range poly {
			facets = append(facets, facetTriangle{center, a, poly[(i+1)%len(poly)], true})
		}
	} else {
		for _, tri := range faceTriangles(poly) {
			mid := poly[tri[0]].Add(poly[tri[1]]).Add(poly[tri[2]]).Mul(1.0 / 3)
			for i, a := range tri {
				b := tri[(i+1)%3]
				facets = append(facets, facetTriangle{mid, poly[a], poly[b], (a+1)%len(poly) == b})
			}
		}
	}
	for _, triangle := range facets {
		a, b := triangle.a, triangle.b
		normal := b.Sub(a).Perp().Norm()
		if triangle.center.Sub(a).Dot(normal) < 0 {
			normal = normal.Mul(-1)
		}
		first := uint32(len(vertices))
		for _, p := range []V{triangle.center, a, b} {
			edgeDistance := .002 // Internal triangulation edges have no rim.
			if triangle.boundary {
				edgeDistance = math.Max(0, p.Sub(a).Dot(normal))
			}
			vertices = append(vertices, ebiten.Vertex{
				DstX: float32(p.X * rasterPixelsPerUnit), DstY: float32((p.Y - generationMinY) * rasterPixelsPerUnit),
				SrcX: float32(p.X * rasterPixelsPerUnit), SrcY: float32((p.Y - generationMinY) * rasterPixelsPerUnit),
				ColorR: float32(clr.R) / 255, ColorG: float32(clr.G) / 255,
				ColorB: float32(clr.B) / 255, ColorA: float32(clr.A) / 255,
				Custom0: float32(edgeDistance * rasterPixelsPerUnit),
				Custom1: float32(p.Sub(center).Dot(tilt) / radius),
				Custom2: float32(surface.X),
				Custom3: float32(surface.Y),
			})
		}
		indices = append(indices, first, first+1, first+2)
	}
	return vertices, indices
}

func edgeKey(a, b V) [4]int64 {
	ax, ay := int64(math.Round(a.X*1e7)), int64(math.Round(a.Y*1e7))
	bx, by := int64(math.Round(b.X*1e7)), int64(math.Round(b.Y*1e7))
	if ax > bx || (ax == bx && ay > by) {
		ax, ay, bx, by = bx, by, ax, ay
	}
	return [4]int64{ax, ay, bx, by}
}

func insetForegroundGrid(grid RockGrid) RockGrid {
	clipped := make(RockGrid, 0, len(grid))
	for _, cell := range grid {
		poly := clipHalfPlane(cell.Polygon, V{-1, 0}, -foregroundScreenInset)
		poly = clipHalfPlane(poly, V{1, 0}, generationWidth-foregroundScreenInset)
		if len(poly) < 3 || faceArea(poly) < 1e-15 {
			continue
		}
		cell.Polygon = poly
		if !insideFace(cell.Center, poly) {
			cell.Center = faceCenter(poly)
		}
		clipped = append(clipped, cell)
	}
	return clipped
}
