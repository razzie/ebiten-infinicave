package main

import (
	_ "embed"
	"flag"
	"image/color"
	"image/png"
	"log"
	"math"
	"math/rand"
	"os"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

const (
	W = 1000
	H = 1000

	seedCount      = 1000
	cellSpacing    = 29.0
	guideSpacing   = 20.0
	guideInfluence = 180.0
	noiseScale     = 0.0034

	showGuides = false
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
func clamp(x, a, b float64) float64 { return math.Max(a, math.Min(b, x)) }
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
	return Guide{Pts: pts, S: ss, BrightSign: sign}
}

func generateGuides(rng *rand.Rand) []Guide {
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
			knots[i] = V{q.X * W, q.Y * H}
		}
		guides = append(guides, splineGuide(knots, 1))
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

func (g *Guide) project(p V) Projection {
	best := Projection{Dist: math.Inf(1)}
	for i := 0; i < len(g.Pts)-1; i++ {
		a, b := g.Pts[i], g.Pts[i+1]
		d := b.Sub(a)
		l2 := d.Len2()
		if l2 == 0 {
			continue
		}
		t := clamp(p.Sub(a).Dot(d)/l2, 0, 1)
		q := a.Add(d.Mul(t))
		delta := p.Sub(q)
		dist := delta.Len()
		if dist >= best.Dist {
			continue
		}

		tan := g.segmentTangent(i, t)
		n := tan.Perp().Mul(g.BrightSign)
		segLen := math.Sqrt(l2)
		best = Projection{
			Q:      q,
			T:      tan,
			N:      n,
			S:      g.S[i] + t*segLen,
			Signed: delta.Dot(n),
			Dist:   dist,
		}
	}
	return best
}

// Interpolate vertex tangents so offset rows do not kink at sample boundaries.
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
		pr := guides[i].project(p)
		if pr.Dist < best.Dist {
			bestI, best = i, pr
		}
	}
	return bestI, best
}

// Negative signed distance is the shadow side selected by the guide normal.
func guideSideSpacing(signed float64) float64 {
	if signed < 0 {
		return guideSpacing * .6
	}
	return guideSpacing * 2.4
}

func desiredSpacing(p V, guides []Guide, noise *Perlin) float64 {
	gi, pr := nearestGuide(p, guides)
	terrain := smoothstep(.39, .58, fbm(noise, p))
	spacing := lerp(43, 30, terrain)
	if gi < 0 {
		return spacing
	}
	return lerp(guideSideSpacing(pr.Signed), spacing, smoothstep(0, 85, pr.Dist))
}

// Follow bends with short polygon edges, without subdividing them into tiny
// slivers. The concept uses broad rock faces even around curled ridges.
func guideSamples(g *Guide, spacing float64, rng *rand.Rand) []float64 {
	length := g.S[len(g.S)-1]
	samples := []float64{0}
	for s := 0.0; s < length; {
		step := math.Min(spacing*lerp(.7, 1, rng.Float64()), length-s)
		for step > 2 {
			a, ta, _ := g.frameAt(s)
			b, tb, _ := g.frameAt(s + step)
			mid, _, _ := g.frameAt(s + step*.5)
			if mid.Sub(lerpV(a, b, .5)).Len() <= 1.2 && ta.Dot(tb) >= math.Cos(.45) {
				break
			}
			step *= .75
		}
		s += step
		samples = append(samples, s)
	}
	// Avoid a tiny final cell at the end of an otherwise regular row.
	if len(samples) > 2 {
		n := len(samples)
		if samples[n-1]-samples[n-2] < .5*(samples[n-2]-samples[n-3]) {
			samples[n-2] = (samples[n-3] + samples[n-1]) * .5
		}
	}
	return samples
}

// Keep a paired crest for a smooth silhouette, then scatter independent bands
// behind it. Unequal lengths and depths produce thin, irregular rock faces
// without continuous horizontal courses or aligned vertical joints.
func addGuideSeeds(seeds []V, guides []Guide, rng *rand.Rand) []V {
	background := make([]V, 0, len(seeds))
	for _, p := range seeds {
		_, pr := nearestGuide(p, guides)
		width := guideSpacing * 2.7
		if pr.Signed >= 0 {
			width = guideSpacing * 4.4
		}
		along := math.Abs(p.Sub(pr.Q).Dot(pr.T))
		if pr.Dist >= width || along > guideSpacing*.5 {
			background = append(background, p)
		}
	}
	seeds = background
	for i := range guides {
		g := &guides[i]
		crest := guideSamples(g, guideSideSpacing(1), rng)
		for _, side := range []float64{-1, 1} {
			offsets := []float64{.4, 1.4, 2.2}
			if side > 0 {
				offsets = []float64{.4, 1.4, 2.5, 3.8}
			}
			for row, offset := range offsets {
				rowSamples := crest
				if row > 0 {
					// Resample every band independently so neighboring cells
					// meet at staggered, irregular polygon edges.
					spacing := guideSideSpacing(side) * (1 + .2*float64(row))
					rowSamples = guideSamples(g, spacing, rng)
				}
				for _, s := range rowSamples {
					depth := guideSpacing * offset
					if row > 0 {
						// Gradually release the normal constraint away from the
						// crest, retaining the flattened shape near the guide.
						depth += guideSpacing * .075 * float64(row) * (2*rng.Float64() - 1)
					}
					q, _, n := g.frameAt(s)
					p := q.Add(n.Mul(side * depth))
					if p.X < 1 || p.X > W-1 || p.Y < 1 || p.Y > H-1 {
						continue
					}
					// Offset curves can fold inside tight curls or meet another
					// guide. Let the nearest guide own that part of the band.
					gi, pr := nearestGuide(p, guides)
					if gi != i || pr.Signed*side <= 0 || math.Abs(pr.S-s) > guideSideSpacing(side) {
						continue
					}
					crowded := false
					for _, seed := range seeds {
						if p.Sub(seed).Len2() < 4 {
							crowded = true
							break
						}
					}
					if !crowded {
						seeds = append(seeds, p)
					}
				}
			}
		}
	}
	return seeds
}

func generateSeeds(rng *rand.Rand, count int, guides []Guide, noise *Perlin) []V {
	// Weighted sampling follows guide-side spacing and background terrain.
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
			spacing := desiredSpacing(p, guides, noise)
			minD2 /= spacing * spacing
			if minD2 > bestD2 {
				best, bestD2 = p, minD2
			}
		}
		pts = append(pts, best)
	}
	return pts
}

func warpSeeds(seeds []V, guides []Guide) {
	row := cellSpacing * 0.92

	for i, p := range seeds {
		gi, pr := nearestGuide(p, guides)
		if gi < 0 || pr.Dist >= guideInfluence {
			continue
		}

		// Guide alignment fades smoothly into the surrounding irregular cells.
		w := 1 - smoothstep(0.35*guideInfluence, guideInfluence, pr.Dist)
		absD := math.Abs(pr.Signed)
		side := 1.0
		if pr.Signed < 0 {
			side = -1
		}

		// Snap normal distance into rows parallel to the guide. The first row is
		// half a cell away, so the guide itself becomes a Voronoi boundary-ish seam.
		rowIndex := math.Max(0, math.Round(absD/row-0.5))
		targetD := side * (rowIndex + 0.5) * row

		// Weak tangent snapping prevents a perfectly crystalline lattice but makes
		// neighboring centers flow smoothly along the guide.
		phase := 0.0
		if int(rowIndex)&1 != 0 {
			phase = 0.5 * row
		}
		targetS := math.Round((pr.S-phase)/row)*row + phase
		q, t, n := guides[gi].frameAt(targetS)
		target := q.Add(n.Mul(targetD))

		// Keep more of the original tangential randomness than normal randomness.
		delta := target.Sub(p)
		nShift := n.Mul(delta.Dot(n) * 0.32 * w)
		tShift := t.Mul(delta.Dot(t) * 0.08 * w)
		p = p.Add(nShift).Add(tShift)
		p.X = clamp(p.X, 2, W-2)
		p.Y = clamp(p.Y, 2, H-2)
		seeds[i] = p
	}
}

// Repel crowded seeds after warping without forcing a regular lattice.
func relaxSeeds(seeds []V, guides []Guide, noise *Perlin) {
	spacing := make([]float64, len(seeds))
	for i, p := range seeds {
		spacing[i] = desiredSpacing(p, guides, noise) * .48
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
type Perlin struct{ p [512]int }

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
	a, f, sum, norm := 1.0, noiseScale, 0.0, 0.0
	for i := 0; i < 4; i++ {
		sum += a * n.Noise(p.X*f, p.Y*f)
		norm += a
		a *= 0.52
		f *= 2.08
	}
	return 0.5 + 0.5*sum/norm
}

func guideBias(p V, guides []Guide, noise *Perlin, branches []BranchSegment) float64 {
	light, shadow := 0.0, 0.0
	branchLight := branchBias(p, branches)
	for i := range guides {
		pr := guides[i].project(p)
		if pr.Dist >= guideInfluence {
			continue
		}
		// Broad noise varies the shoulder beneath the branching light field.
		// The normal stays continuous around tight curves.
		variation := smoothstep(.32, .66, fbm(noise, p.Add(V{317, 791})))
		width := lerp(48, guideInfluence, variation)
		along := math.Sqrt(math.Max(0, pr.Dist*pr.Dist-pr.Signed*pr.Signed))
		endFade := 1 - smoothstep(0, width*.45, along)
		if pr.Signed >= 0 {
			crest := .64 * math.Exp(-pr.Dist/lerp(15, 23, variation))
			shoulder := .25 * (1 - smoothstep(0, width, pr.Dist))
			light = math.Max(light, (crest+shoulder)*endFade)
		} else {
			// Offshoots can approach another ridge, but must not light its
			// dense shadow cells or spill across the original crest.
			branchLight *= smoothstep(guideSpacing*2.7, guideSpacing*5, pr.Dist)
			shadow = math.Max(shadow, .24*(1-smoothstep(0, width*.8, pr.Dist))*endFade)
		}
	}
	return math.Max(light, branchLight) - shadow
}

func cellColor(v float64) color.NRGBA {
	// Base tones for the material: charcoal, gray, and warm ivory.
	stops := []struct {
		value   float64
		r, g, b float64
	}{
		{0, 0, 0, 0},
		{.10, 3, 3, 3},
		{.25, 37, 38, 37},
		{.43, 83, 83, 79},
		{.65, 145, 143, 133},
		{.85, 197, 192, 177},
		{1, 221, 215, 200},
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
	return cellColor(.30 * smoothstep(.43, .59, fbm(noise, p)))
}

// Unlit foreground cells are absent. Fade the outer shoulders and branch tips
// per cell so the independent dark tessellation shows through their faces.
func guideCellColor(p V, guides []Guide, noise *Perlin, branches []BranchSegment) color.NRGBA {
	strength := guideBias(p, guides, noise, branches)
	if strength <= 0 {
		return color.NRGBA{}
	}
	clr := cellColor(.18 + strength)
	clr.A = uint8(math.Round(255 * smoothstep(0, .24, strength)))
	return clr
}

func clipHalfPlane(poly []V, n V, c float64) []V {
	if len(poly) == 0 {
		return poly
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
	return poly
}

// Convex Voronoi cells are triangulated around their interior seed. Each
// triangle carries its distance to the outer edge for the narrow material rim.
func appendCellMesh(vertices []ebiten.Vertex, indices []uint32, poly []V, center V, clr color.NRGBA, rng *rand.Rand) ([]ebiten.Vertex, []uint32) {
	if len(poly) < 3 {
		return vertices, indices
	}
	radius := 0.0
	for _, p := range poly {
		radius = math.Max(radius, p.Sub(center).Len())
	}
	radius = math.Max(radius, 1)
	angle := rng.Float64() * 2 * math.Pi
	tilt := V{math.Cos(angle), math.Sin(angle)}
	light := V{-.6, -.8}
	for i, a := range poly {
		b := poly[(i+1)%len(poly)]
		normal := b.Sub(a).Perp().Norm()
		if center.Sub(a).Dot(normal) < 0 {
			normal = normal.Mul(-1)
		}
		facet := rng.Float64() - .5
		first := uint32(len(vertices))
		for _, p := range []V{center, a, b} {
			vertices = append(vertices, ebiten.Vertex{
				DstX: float32(p.X), DstY: float32(p.Y),
				SrcX: float32(p.X), SrcY: float32(p.Y),
				ColorR: float32(clr.R) / 255, ColorG: float32(clr.G) / 255,
				ColorB: float32(clr.B) / 255, ColorA: float32(clr.A) / 255,
				Custom0: float32(math.Max(0, p.Sub(a).Dot(normal))),
				Custom1: float32(p.Sub(center).Dot(tilt) / radius),
				Custom2: float32(facet),
				Custom3: float32(-normal.Dot(light)),
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
	canvas    *ebiten.Image
	material  *ebiten.Shader
	texture   float64
	seed      int64
	output    string
	exported  bool
	exportErr error
}

// Render each tessellation separately so transparent guide cells contribute
// neither faces nor outlines, and the foreground covers the background mesh.
func (g *Game) drawGrid(img *ebiten.Image, seeds []V, cellColorAt func(V) color.NRGBA, rng *rand.Rand) {
	vertices := make([]ebiten.Vertex, 0, len(seeds)*18)
	indices := make([]uint32, 0, len(seeds)*18)
	type cellEdge struct {
		a, b  V
		alpha uint8
	}
	edges := make(map[[4]int64]cellEdge)
	hiddenEdges := make(map[[4]int64]bool)
	for i, s := range seeds {
		clr := cellColorAt(s)
		if clr.A == 0 {
			continue
		}
		poly := voronoiCell(i, seeds)
		vertices, indices = appendCellMesh(vertices, indices, poly, s, clr, rng)
		for j, a := range poly {
			b := poly[(j+1)%len(poly)]
			key := edgeKey(a, b)
			// Preserve quiet black pockets in the underlying grid.
			if clr.A == 255 && clr.R <= 3 && clr.G <= 3 && clr.B <= 3 {
				hiddenEdges[key] = true
			}
			alpha := uint8(math.Round(55 * float64(clr.A) / 255))
			if previous, ok := edges[key]; !ok || alpha > previous.alpha {
				edges[key] = cellEdge{a, b, alpha}
			}
		}
	}

	if len(indices) == 0 {
		return
	}
	img.DrawTrianglesShader32(vertices, indices, g.material, &ebiten.DrawTrianglesShaderOptions{
		AntiAlias: true,
		Uniforms: map[string]any{
			"Texture": float32(g.texture),
			"Offset":  []float32{rng.Float32() * 1000, rng.Float32() * 1000},
		},
	})

	// Shared edges are stroked once, using the more visible adjacent cell.
	// Outline opacity follows the faces, including at transparent boundaries.
	var paths [56]*vector.Path
	for key, edge := range edges {
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
			strokePath(img, path, .55, color.NRGBA{R: 158, G: 158, B: 151, A: uint8(alpha)})
		}
	}
}

func (g *Game) regenerate() {
	// Separate random streams keep the dark grid independent of guide edits.
	backgroundRNG := rand.New(rand.NewSource(g.seed ^ 0x62617365))
	backgroundNoise := NewPerlin(backgroundRNG)
	backgroundSeeds := generateSeeds(backgroundRNG, seedCount, nil, backgroundNoise)
	relaxSeeds(backgroundSeeds, nil, backgroundNoise)

	rng := rand.New(rand.NewSource(g.seed))
	guides := generateGuides(rng)
	noise := NewPerlin(rng)
	seeds := generateSeeds(rng, seedCount, guides, noise)
	warpSeeds(seeds, guides)
	relaxSeeds(seeds, guides, noise)
	seeds = addGuideSeeds(seeds, guides, rng)
	branches := generateBranches(guides, noise, rand.New(rand.NewSource(g.seed^0x6272616e6368)))

	img := ebiten.NewImage(W, H)
	img.Fill(color.Black)
	g.drawGrid(img, backgroundSeeds, func(p V) color.NRGBA {
		return backgroundCellColor(p, backgroundNoise)
	}, backgroundRNG)

	foreground := ebiten.NewImage(W, H)
	g.drawGrid(foreground, seeds, func(p V) color.NRGBA {
		return guideCellColor(p, guides, noise, branches)
	}, rng)
	img.DrawImage(foreground, nil)
	foreground.Deallocate()

	if showGuides {
		blue := color.NRGBA{R: 0, G: 160, B: 250, A: 255}
		for i := range guides {
			p := &vector.Path{}
			p.MoveTo(float32(guides[i].Pts[0].X), float32(guides[i].Pts[0].Y))
			for _, q := range guides[i].Pts[1:] {
				p.LineTo(float32(q.X), float32(q.Y))
			}
			strokePath(img, p, 7.5, blue)
		}
	}

	if g.canvas != nil {
		g.canvas.Deallocate()
	}
	g.canvas = img
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
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	screen.DrawImage(g.canvas, nil)
	if g.output != "" && !g.exported {
		g.exported = true
		f, err := os.Create(g.output)
		if err != nil {
			g.exportErr = err
			return
		}
		g.exportErr = png.Encode(f, g.canvas)
		if err := f.Close(); g.exportErr == nil {
			g.exportErr = err
		}
	}
}

func (g *Game) Layout(_, _ int) (int, int) { return W, H }

func main() {
	seed := flag.Int64("seed", rand.Int63(), "random seed (random by default)")
	output := flag.String("output", "", "save a PNG to this path and exit")
	texture := flag.Float64("texture", 1, "surface texture strength (0 disables it, range 0-2)")
	flag.Parse()
	if math.IsNaN(*texture) || *texture < 0 || *texture > 2 {
		log.Fatal("texture must be between 0 and 2")
	}
	material, err := ebiten.NewShader(materialShaderSource)
	if err != nil {
		log.Fatal(err)
	}
	defer material.Deallocate()

	g := &Game{seed: *seed, output: *output, material: material, texture: *texture}
	g.regenerate()

	ebiten.SetWindowSize(W, H)
	ebiten.SetWindowTitle("Perlin + guide-warped Voronoi")
	if err := ebiten.RunGame(g); err != nil {
		log.Fatal(err)
	}
}
