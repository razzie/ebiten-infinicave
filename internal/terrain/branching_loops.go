package terrain

import (
	"math"
	"math/rand"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// Prefer complete cycles to dead-end limbs. Main routes reconnect to a guide;
// secondary routes reconnect to an existing limb. The latter form shared walls
// and additional chambers instead of filling the main opening with tree roots.
// Each guide has a bounded number of proposals, independent of window order.
func appendLoops(branches []BranchSegment, g *Guide, guides []Guide, noise *Perlin, rng *rand.Rand, options ...ShapeOptions) []BranchSegment {
	shape := DefaultShapeOptions()
	if len(options) > 0 {
		shape = options[0]
	}
	length := g.S[len(g.S)-1]
	for s := geom.Lerp(.045, .08, rng.Float64()); s+.24 < length-.04; {
		span := math.Min(geom.Lerp(.28, .46, rng.Float64()), length-.04-s)
		path := bestLoopRoute(g, s, s+span, .045, branches, guides, noise, rng, shape)
		if len(path) == 0 {
			// Retry nearby roots rather than letting one blocked proposal
			// consume all the available guide arc.
			s += .12
			continue
		}
		branches = append(branches, path...)
		// Unequal attachment intervals make side chambers of different sizes.
		for _, interval := range [][2]float64{{.12, .58}, {.43, .9}} {
			first := int(interval[0] * float64(len(path)))
			last := min(len(path)-1, int(interval[1]*float64(len(path))))
			pts := []geom.V{path[first].A}
			for _, seg := range path[first : last+1] {
				pts = append(pts, seg.B)
			}
			base, _ := loadedGuide(Guide{Pts: pts, BrightSign: 1})
			mid, _, normal := base.frameAt(base.S[len(base.S)-1] / 2)
			if normal.Dot(g.project(mid).N) < 0 {
				base.BrightSign = -1
			}
			if base.S[len(base.S)-1] < .20 {
				continue
			}
			detour := bestLoopRoute(&base, 0, base.S[len(base.S)-1], 0, branches, guides, noise, rng, shape)
			branches = append(branches, detour...)
		}
		s += span * geom.Lerp(.85, 1.05, rng.Float64())
	}
	return branches
}

// Search several asymmetric, multi-bend routes and retain the one with the
// most usable open area. Shorter attempts can fit crowded guide neighborhoods.
// Checking the finished route and its opening prevents invisible bridges,
// self-crossing ribbons, and cycles whose hole disappears under the rock width.
func bestLoopRoute(base *Guide, from, to, inset float64, existing []BranchSegment, guides []Guide, noise *Perlin, rng *rand.Rand, shape ShapeOptions) []BranchSegment {
	a, _, na := base.frameAt(from)
	b, _, nb := base.frameAt(to)
	a, b = a.Add(na.Mul(inset)), b.Add(nb.Mul(inset))
	if inset > 0 && (reliefHeight(a, guides, noise, nil) <= rockContourHeight || reliefHeight(b, guides, noise, nil) <= rockContourHeight) {
		return nil
	}
	var best []BranchSegment
	bestScore := 0.0
	for attempt := 0; attempt < 6; attempt++ {
		reach := geom.Lerp(.19, .39, rng.Float64()) * (1 - float64(attempt)*.07)
		if inset == 0 {
			reach *= .8
		}
		knots := []geom.V{a}
		count := 5 + rng.Intn(3)
		for j := 1; j < count; j++ {
			u := (float64(j) + geom.Lerp(-.18, .18, rng.Float64())) / float64(count)
			q, tangent, normal := base.frameAt(geom.Lerp(from, to, u))
			envelope := math.Pow(math.Sin(math.Pi*u), .65)
			// Independent heights, unequal spacing and tangential drift make
			// lobes and necks rather than a symmetric cubic arch.
			height := inset + reach*envelope*geom.Lerp(.62, 1.25, rng.Float64())
			drift := geom.Lerp(-.045, .045, rng.Float64()) * envelope
			knots = append(knots, q.Add(normal.Mul(height)).Add(tangent.Mul(drift)))
		}
		knots = append(knots, b)
		curve := SplineGuide(knots, 1)
		path := sampleLoopRoute(&curve, existing, guides, noise, shape)
		if len(path) == 0 {
			continue
		}
		area := loopOpenArea(path, base, from, to, inset, existing, guides, noise)
		if area < .003 {
			continue
		}
		turn := 0.0
		for i := 1; i < len(path); i++ {
			u, v := path[i-1].B.Sub(path[i-1].A).Norm(), path[i].B.Sub(path[i].A).Norm()
			turn += math.Abs(math.Atan2(geom.Cross(u, v), u.Dot(v)))
		}
		score := area * (1 + .1*math.Min(turn, 6))
		if score > bestScore {
			best, bestScore = path, score
		}
	}
	return best
}

func sampleLoopRoute(curve *Guide, existing []BranchSegment, guides []Guide, noise *Perlin, shape ShapeOptions) []BranchSegment {
	length := curve.S[len(curve.S)-1]
	steps := int(math.Ceil(length / .008))
	path := make([]BranchSegment, 0, steps)
	p := curve.Pts[0]
	widthAt := func(p geom.V) float64 {
		return shape.branchWidth(geom.Clamp(.5+.5*noise.Noise(p.X*7+113, (p.Y+noise.OffsetY)*7+59), 0, 1))
	}
	wa := widthAt(p)
	for j := 1; j <= steps; j++ {
		q, _, _ := curve.frameAt(length * float64(j) / float64(steps))
		wb := widthAt(q)
		if !branchCanGrow(p, .5, wa, guides) || !branchCanGrow(q, .5, wb, guides) || !branchCanGrow(geom.LerpVector(p, q, .5), .5, (wa+wb)*.5, guides) {
			return nil
		}
		// Leave room between distant parts of the same ribbon. Adjacent
		// samples necessarily overlap and are exempt from this test.
		for i := 0; i+12 < len(path); i++ {
			if geom.SegmentDistanceSquared(p, q, path[i].A, path[i].B) < math.Pow((math.Max(wa, wb)+math.Max(path[i].WidthA, path[i].WidthB))*1.25, 2) {
				return nil
			}
		}
		// Secondary routes may attach at either end, but must not cut
		// through the network in between and erase an existing chamber.
		if len(existing) > 0 && j > 12 && j < steps-12 {
			for _, seg := range existing {
				if geom.SegmentDistanceSquared(p, q, seg.A, seg.B) < math.Pow((math.Max(wa, wb)+math.Max(seg.WidthA, seg.WidthB))*1.25, 2) {
					return nil
				}
			}
		}
		path = append(path, BranchSegment{A: p, B: q, WidthA: wa, WidthB: wb, LightA: .5, LightB: .5})
		p, wa = q, wb
	}
	return path
}

func loopOpenArea(path []BranchSegment, base *Guide, from, to, inset float64, existing []BranchSegment, guides []Guide, noise *Perlin) float64 {
	outline := []geom.V{path[0].A}
	for _, seg := range path {
		outline = append(outline, seg.B)
	}
	for j := 24; j >= 0; j-- {
		p, _, normal := base.frameAt(geom.Lerp(from, to, float64(j)/24))
		outline = append(outline, p.Add(normal.Mul(inset)))
	}
	lo, hi := geom.PolygonBounds(outline)
	field := newBranchField(append(append([]BranchSegment(nil), existing...), path...))
	const side = 12
	open := 0
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			p := geom.V{X: geom.Lerp(lo.X, hi.X, (float64(x)+.5)/side), Y: geom.Lerp(lo.Y, hi.Y, (float64(y)+.5)/side)}
			if geom.InsidePolygon(p, outline) && reliefHeight(p, guides, noise, field) < rockContourHeight {
				open++
			}
		}
	}
	return (hi.X - lo.X) * (hi.Y - lo.Y) * float64(open) / (side * side)
}
