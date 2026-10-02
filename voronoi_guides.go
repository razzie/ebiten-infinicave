package main

import (
	_ "embed"
	"image/color"
	"image/png"
	"math"
	"math/rand"
	"os"
	"sort"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

const (
	W                     = 1000
	H                     = 3 * W // local generation window: a section plus padding above and below
	foregroundScreenInset = 18.0

	guideSpacing   = 20.0
	guideInfluence = 180.0
	noiseScale     = 0.0034
)

//go:embed grain.kage
var materialShaderSource []byte

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
	pts := []V{knots[0]}
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

func generateGuides(rng *rand.Rand) []Guide {
	var guides []Guide
	for top := 0; top < H; top += W {
		section := generateGuideSection(rng)
		for i := range section {
			section[i].translateY(float64(top))
		}
		guides = append(guides, section...)
	}
	return guides
}

func generateGuideSection(rng *rand.Rand) []Guide {
	// Unequal compositional anchors leave large pockets of negative space.
	// Curves may cross regions or leave the canvas. Knot order sets the lit side.
	templates := [][]V{
		{{.24, .38}, {.43, .30}, {.62, .21}, {.73, .12}, {.76, .02}},
		{{.26, .08}, {.34, .05}, {.43, -.015}},
		{{.035, .15}, {.10, .19}, {.18, .205}},
		{{.80, .125}, {.85, .08}, {.90, .025}},
		{{-.025, .45}, {.095, .49}, {.15, .52}},
		{{.10, .65}, {.26, .625}, {.44, .60}},
		{{.50, .62}, {.55, .56}, {.565, .48}, {.545, .44}, {.505, .415}, {.535, .38}, {.585, .35}},
		{{.76, .595}, {.84, .525}, {.90, .455}, {.87, .385}},
		{{.635, .785}, {.602, .748}, {.615, .705}, {.65, .677}, {.71, .697}},
		{{.445, .925}, {.49, .83}, {.535, .735}},
		{{.56, .75}, {.60, .82}, {.68, .86}, {.80, .94}},
	}
	guides := make([]Guide, 0, len(templates))

	for _, template := range templates {
		center := V{}
		for _, p := range template {
			center = center.Add(p)
		}
		center = center.Mul(1 / float64(len(template)))
		shift := V{lerp(-.018, .018, rng.Float64()), lerp(-.018, .018, rng.Float64())}
		scale := lerp(.91, 1.09, rng.Float64())
		angle := lerp(-.07, .07, rng.Float64())
		knots := make([]V, len(template))
		for i, p := range template {
			d := p.Sub(center).Mul(scale)
			d = V{d.X*math.Cos(angle) - d.Y*math.Sin(angle), d.X*math.Sin(angle) + d.Y*math.Cos(angle)}
			q := center.Add(shift).Add(d)
			knots[i] = V{q.X * W, q.Y * W}
		}
		// Derive a separate stream without changing the compositional anchors.
		g := splineGuide(knots, 1)
		seed := sectionSeed(int64(math.Round(knots[0].X*1000)), int64(math.Round(knots[0].Y*1000)))
		guides = append(guides, ridgedGuide(g, seed))
	}
	return guides
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
	return lerp(30, 22, smoothstep(.39, .58, fbm(noise, p)))
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
			p := V{rng.Float64() * W, rng.Float64() * H}
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
			seeds[i].X = clamp(seeds[i].X, 1, W-1)
			seeds[i].Y = clamp(seeds[i].Y, 1, H-1)
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
	return cellColor(.30 * smoothstep(.43, .59, fbm(noise, p)) * (.4 + .6*surfaceLight(normal)))
}

// Rock occupancy follows relief, independently of light. Every existing face
// is opaque, including its dark flank and the tapered ends of raised spurs.
func guideCellColor(p V, guides []Guide, noise *Perlin, branches *BranchField) color.NRGBA {
	dx := reliefHeight(p.Add(V{2, 0}), guides, noise, branches) - reliefHeight(p.Sub(V{2, 0}), guides, noise, branches)
	dy := reliefHeight(p.Add(V{0, 2}), guides, noise, branches) - reliefHeight(p.Sub(V{0, 2}), guides, noise, branches)
	return guideSurfaceColor(p, guides, noise, branches, (V3{-dx / 4, -dy / 4, 1}).Norm())
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
		if p.Dot(n) <= c+1e-9 {
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
	prevIn := prev.Dot(n) <= c+1e-9
	for _, cur := range poly {
		curIn := cur.Dot(n) <= c+1e-9
		if curIn != prevIn {
			d := cur.Sub(prev)
			den := d.Dot(n)
			if math.Abs(den) > 1e-12 {
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
	poly := []V{{0, 0}, {W, 0}, {W, H}, {0, H}}
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
			ax, bx := math.Round(poly[j].X*1e6), math.Round(poly[first].X*1e6)
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
	radius = math.Max(radius, 1)
	tilt := V{surface.X, surface.Y}
	type facetTriangle struct {
		center, a, b V
		boundary     bool
	}
	var facets []facetTriangle
	fan := true
	for i, a := range poly {
		if cross(poly[(i+1)%len(poly)].Sub(a), center.Sub(a)) < -1e-9 {
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
			edgeDistance := 2.0 // Internal triangulation edges have no rim.
			if triangle.boundary {
				edgeDistance = math.Max(0, p.Sub(a).Dot(normal))
			}
			vertices = append(vertices, ebiten.Vertex{
				DstX: float32(p.X), DstY: float32(p.Y),
				SrcX: float32(p.X), SrcY: float32(p.Y),
				ColorR: float32(clr.R) / 255, ColorG: float32(clr.G) / 255,
				ColorB: float32(clr.B) / 255, ColorA: float32(clr.A) / 255,
				Custom0: float32(edgeDistance),
				Custom1: float32(p.Sub(center).Dot(tilt) / radius),
				Custom2: float32(surface.X),
				Custom3: float32(surface.Y),
			})
		}
		indices = append(indices, first, first+1, first+2)
	}
	return vertices, indices
}

func strokePath(dst *ebiten.Image, p *vector.Path, width float32, clr color.Color) {
	op := &vector.DrawPathOptions{AntiAlias: true}
	op.ColorScale.ScaleWithColor(clr)
	vector.StrokePath(dst, p, &vector.StrokeOptions{
		Width:    width,
		LineCap:  vector.LineCapButt,
		LineJoin: vector.LineJoinRound,
	}, op)
}

func edgeKey(a, b V) [4]int64 {
	ax, ay := int64(math.Round(a.X*1e4)), int64(math.Round(a.Y*1e4))
	bx, by := int64(math.Round(b.X*1e4)), int64(math.Round(b.Y*1e4))
	if ax > bx || (ax == bx && ay > by) {
		ax, ay, bx, by = bx, by, ax, ay
	}
	return [4]int64{ax, ay, bx, by}
}

type Game struct {
	camera      Camera
	world       *World
	loading     bool
	material    *ebiten.Shader
	texture     float64
	seed        int64
	output      string
	study, view string
	exported    bool
	exportErr   error
}

// Render each tessellation separately so transparent guide cells contribute
// neither faces nor outlines, and the foreground covers the background mesh.
func (g *Game) drawGrid(img *ebiten.Image, grid RockGrid, top float64) {
	if len(grid) > 0 && grid[0].Raised {
		grid = insetForegroundGrid(grid)
	}
	vertices := make([]ebiten.Vertex, 0, len(grid)*18)
	indices := make([]uint32, 0, len(grid)*18)
	var boundary []rockEdge
	if len(grid) > 0 && grid[0].Raised && (g.view == "shaded" || g.view == "" || g.view == "clay") {
		boundary = exposedRockEdges(grid)
		vertices, indices = appendRockWalls(vertices, indices, grid, boundary, g.view)
	}
	type cellEdge struct {
		a, b  V
		alpha uint8
	}
	edges := make(map[[4]int64]cellEdge)
	hiddenEdges := make(map[[4]int64]bool)
	for _, cell := range grid {
		s, clr, poly := cell.Center, rockViewColor(cell, g.view), cell.Polygon
		if clr.A == 0 {
			continue
		}
		vertices, indices = appendCellMesh(vertices, indices, poly, s, clr, cell.Normal)
		for j, a := range poly {
			b := poly[(j+1)%len(poly)]
			key := edgeKey(a, b)
			// Preserve quiet black pockets in the underlying grid.
			if clr.A == 255 && clr.R <= 3 && clr.G <= 3 && clr.B <= 3 {
				hiddenEdges[key] = true
			}
			alpha := uint8(math.Round((96 + 28*surfaceLight(cell.Normal)) * float64(clr.A) / 255))
			if g.view != "shaded" && g.view != "" {
				alpha = 0
			}
			if previous, ok := edges[key]; !ok || alpha > previous.alpha {
				edges[key] = cellEdge{a, b, alpha}
			}
		}
	}

	vertices, indices = appendRockBevels(vertices, indices, grid, boundary, g.view)
	if len(indices) == 0 {
		return
	}
	img.DrawTrianglesShader32(vertices, indices, g.material, &ebiten.DrawTrianglesShaderOptions{
		AntiAlias: true,
		Uniforms: map[string]any{
			"Texture": float32(g.texture),
			"Offset":  []float32{317, float32(top) + 791},
		},
	})

	// Shared edges are stroked once, using the more visible adjacent cell.
	// Outline opacity follows the faces, including at transparent boundaries.
	var paths [256]*vector.Path
	// Stable path order also keeps antialiasing identical after cache eviction.
	keys := make([][4]int64, 0, len(edges))
	for key := range edges {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		for k := 0; k < 4; k++ {
			if keys[i][k] != keys[j][k] {
				return keys[i][k] < keys[j][k]
			}
		}
		return false
	})
	for _, key := range keys {
		edge := edges[key]
		if hiddenEdges[key] || edge.alpha == 0 {
			continue
		}
		if paths[edge.alpha] == nil {
			paths[edge.alpha] = &vector.Path{}
		}
		path := paths[edge.alpha]
		path.MoveTo(float32(edge.a.X), float32(edge.a.Y))
		path.LineTo(float32(edge.b.X), float32(edge.b.Y))
	}
	for alpha, path := range paths {
		if path != nil {
			strokePath(img, path, 1.05, color.NRGBA{R: 18, G: 18, B: 17, A: uint8(alpha)})
		}
	}
}

func insetForegroundGrid(grid RockGrid) RockGrid {
	clipped := make(RockGrid, 0, len(grid))
	for _, cell := range grid {
		poly := clipHalfPlane(cell.Polygon, V{-1, 0}, -foregroundScreenInset)
		poly = clipHalfPlane(poly, V{1, 0}, W-foregroundScreenInset)
		if len(poly) < 3 || faceArea(poly) < 1e-9 {
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

func (g *Game) regenerate() {
	if g.world != nil {
		g.world.close()
	}
	g.world = newWorld(g.seed, g.study)
}

func (g *Game) Update() error {
	if g.exported {
		if g.exportErr != nil {
			return g.exportErr
		}
		return ebiten.Termination
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyR) {
		g.seed++
		g.regenerate()
	}
	g.world.receive(g)
	g.updateCamera()
	g.camera.step()
	g.loading = !g.world.ensure(g.camera.Y, g.camera.Height, g.camera.Velocity)
	g.world.prune(g.camera.Y, g.camera.Height, g.camera.Velocity)
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	g.world.draw(screen, g.camera.Y, g.camera.Height)
	if g.loading {
		ebitenutil.DebugPrint(screen, "Growing upward...")
	}
	if g.output != "" && !g.exported && !g.loading {
		g.exported = true
		img := ebiten.NewImage(W, exportHeight)
		defer img.Deallocate()
		g.world.draw(img, -exportHeight, exportHeight)
		f, err := os.Create(g.output)
		if err != nil {
			g.exportErr = err
			return
		}
		g.exportErr = png.Encode(f, img)
		if err := f.Close(); g.exportErr == nil {
			g.exportErr = err
		}
	}
}
