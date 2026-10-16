package terrain

import (
	"fmt"
	"math"
	"math/rand"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// BranchSegment carries a tapering raised spur through the existing cells.
// LightA/B are the historical strength values, now used as relief amplitude.
type BranchSegment struct {
	A, B           geom.V
	WidthA, WidthB float64
	LightA, LightB float64
}

func generateBranches(guides []Guide, noise *Perlin, rng *rand.Rand, options ...ShapeOptions) []BranchSegment {
	shape := DefaultShapeOptions()
	if len(options) > 0 {
		shape = options[0]
	}
	var branches []BranchSegment
	for i := range guides {
		g := &guides[i]
		if g.Seed != 0 {
			rng = rand.New(rand.NewSource(g.Seed ^ 0x6272616e6368))
		}
		length := g.S[len(g.S)-1]
		count := int(math.Max(1, math.Round(length/0.115)))
		for j := 0; j < count; j++ {
			// Stratified roots leave gaps between the spreading shoulders.
			s := length * (float64(j) + geom.Lerp(.25, .75, rng.Float64())) / float64(count)
			q, tangent, normal := g.frameAt(s)
			// Start inside even the narrowest ridge flank. A root beyond its
			// footprint can create a detached spur before any guide masks it.
			root := q.Add(normal.Mul(geom.Lerp(0.04, 0.055, rng.Float64())))
			direction := normal.Add(tangent.Mul(geom.Lerp(-.65, .65, rng.Float64()))).Norm()
			reach := geom.Lerp(0.17, 0.3, rng.Float64())
			width := shape.branchWidth(rng.Float64())
			phase := rng.Float64() * 1000 // Dimensionless noise phase, independent of scene scale.
			forkAt := geom.Lerp(.38, .60, rng.Float64())
			forked := false
			p := root
			if reliefHeight(root, guides[i:i+1], noise, nil) <= rockContourHeight || !branchCanGrow(p, .50, width, guides) {
				continue
			}
			for d := 0.0; d < reach; {
				next := math.Min(d+0.012, reach)
				u, v := d/reach, next/reach
				// A coherent lateral drift makes connected fingers instead of
				// isolated patches from thresholding a second noise field.
				bend := noise.Noise(p.X*9+phase, (p.Y+noise.OffsetY)*9-phase)
				heading := direction.Add(direction.Perp().Mul(bend * 1.5)).Norm()
				end := p.Add(heading.Mul(next - d))
				lightA := .50 * (1 - geom.Smoothstep(.12, 1, u))
				lightB := .50 * (1 - geom.Smoothstep(.12, 1, v))
				widthA, widthB := width*geom.Lerp(1, .22, u), width*geom.Lerp(1, .22, v)
				if !branchCanGrow(end, lightB, widthB, guides) || !branchCanGrow(geom.LerpVector(p, end, .5), (lightA+lightB)*.5, (widthA+widthB)*.5, guides) {
					break
				}
				branches = append(branches, BranchSegment{
					A: p, B: end,
					WidthA: widthA, WidthB: widthB,
					LightA: lightA, LightB: lightB,
				})
				if !forked && v >= forkAt {
					forked = true
					side := 1.0
					if rng.Intn(2) == 0 {
						side = -1
					}
					forkDir := heading.Add(heading.Perp().Mul(side * .95)).Norm()
					branches = appendBranchFork(branches, end, forkDir, reach*geom.Lerp(.35, .55, rng.Float64()), math.Max(shape.branchWidth(0), width*.55), .425*(1-geom.Smoothstep(.12, 1, v)), phase, noise, guides)
				}
				p, d = end, next
			}
		}
	}
	return branches
}

func appendBranchFork(branches []BranchSegment, p, direction geom.V, reach, width, light, phase float64, noise *Perlin, guides []Guide) []BranchSegment {
	if !branchCanGrow(p, light, width, guides) {
		return branches
	}
	for d := 0.0; d < reach; {
		next := math.Min(d+0.012, reach)
		u, v := d/reach, next/reach
		bend := noise.Noise(p.X*12+phase, (p.Y+noise.OffsetY)*12-phase)
		heading := direction.Add(direction.Perp().Mul(bend)).Norm()
		end := p.Add(heading.Mul(next - d))
		lightA := light * (1 - geom.Smoothstep(0, 1, u))
		lightB := light * (1 - geom.Smoothstep(0, 1, v))
		widthA, widthB := width*geom.Lerp(1, .25, u), width*geom.Lerp(1, .25, v)
		if !branchCanGrow(end, lightB, widthB, guides) || !branchCanGrow(geom.LerpVector(p, end, .5), (lightA+lightB)*.5, (widthA+widthB)*.5, guides) {
			break
		}
		branches = append(branches, BranchSegment{
			A: p, B: end,
			WidthA: widthA, WidthB: widthB,
			LightA: lightA, LightB: lightB,
		})
		p, d = end, next
	}
	return branches
}

// A masked centerline must end here: continuing through the invisible band
// lets a later segment reappear as an isolated rock chip. Check during growth,
// before the relief is sampled into faces, for both trunks and forks.
func branchCanGrow(p geom.V, light, width float64, guides []Guide) bool {
	edgeFade := geom.Smoothstep(0, foregroundEdgeFadeWidth, math.Min(p.X, generationWidth-p.X))
	// A spur thinner than the face sampling can disappear in one face and
	// reappear in the next. Stop before its solid width falls below ordinary
	// site spacing, allowing for the negative chip noise in reliefHeight.
	shoulder := 1 - geom.Smoothstep(width*.15, width*1.8, maxRockSeedSpacing/2)
	if (.08*light*shoulder-rockReliefChipHeight)*edgeFade <= rockContourHeight {
		return false
	}
	const shadowRadius = guideSpacing * 5
	for i := range guides {
		g := &guides[i]
		if g.distanceBound2(p) >= shadowRadius*shadowRadius {
			continue
		}
		pr := g.project(p)
		if pr.Signed < 0 && pr.Dist < shadowRadius {
			return false
		}
	}
	return true
}

const branchCell = .032

// BranchField buckets segments by padded bounds; a spur contributes nothing
// outside its support, and the max in branchBias is order independent.
type BranchField struct {
	negative   *BranchField
	segs       []BranchSegment
	lo, hi     []geom.V
	cols, rows int
	origin     geom.V
	cells      [][]int32
}

func newBranchField(segs []BranchSegment) *BranchField {
	f := &BranchField{segs: segs, lo: make([]geom.V, len(segs)), hi: make([]geom.V, len(segs))}
	if len(segs) == 0 {
		return f
	}
	min, max := geom.V{X: math.Inf(1), Y: math.Inf(1)}, geom.V{X: math.Inf(-1), Y: math.Inf(-1)}
	for i, b := range segs {
		pad := math.Max(b.WidthA, b.WidthB) * 1.8
		f.lo[i] = geom.V{X: math.Min(b.A.X, b.B.X) - pad, Y: math.Min(b.A.Y, b.B.Y) - pad}
		f.hi[i] = geom.V{X: math.Max(b.A.X, b.B.X) + pad, Y: math.Max(b.A.Y, b.B.Y) + pad}
		min = geom.V{X: math.Min(min.X, f.lo[i].X), Y: math.Min(min.Y, f.lo[i].Y)}
		max = geom.V{X: math.Max(max.X, f.hi[i].X), Y: math.Max(max.Y, f.hi[i].Y)}
	}
	f.origin = min
	f.cols, f.rows = int((max.X-min.X)/branchCell)+1, int((max.Y-min.Y)/branchCell)+1
	f.cells = make([][]int32, f.cols*f.rows)
	for i := range segs {
		x0, x1 := int((f.lo[i].X-min.X)/branchCell), int((f.hi[i].X-min.X)/branchCell)
		y0, y1 := int((f.lo[i].Y-min.Y)/branchCell), int((f.hi[i].Y-min.Y)/branchCell)
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				f.cells[y*f.cols+x] = append(f.cells[y*f.cols+x], int32(i))
			}
		}
	}
	return f
}

func branchBias(p geom.V, field *BranchField) float64 {
	light := 0.0
	if field == nil || len(field.segs) == 0 {
		return light
	}
	x, y := int(math.Floor((p.X-field.origin.X)/branchCell)), int(math.Floor((p.Y-field.origin.Y)/branchCell))
	if x < 0 || y < 0 || x >= field.cols || y >= field.rows {
		return light
	}
	for _, idx := range field.cells[y*field.cols+x] {
		b := field.segs[idx]
		if p.X < field.lo[idx].X || p.X > field.hi[idx].X || p.Y < field.lo[idx].Y || p.Y > field.hi[idx].Y {
			continue
		}
		delta := b.B.Sub(b.A)
		u := 0.0
		if delta.Len2() > 0 {
			u = geom.Clamp(p.Sub(b.A).Dot(delta)/delta.Len2(), 0, 1)
		}
		distance := p.Sub(geom.LerpVector(b.A, b.B, u)).Len()
		width := geom.Lerp(b.WidthA, b.WidthB, u)
		strength := geom.Lerp(b.LightA, b.LightB, u)
		// Max preserves dark gaps and avoids overexposing intersecting limbs.
		light = math.Max(light, strength*(1-geom.Smoothstep(width*.15, width*1.8, distance)))
	}
	return light
}

func generateBranchesWithStrategy(guides []Guide, noise *Perlin, rng *rand.Rand, strategy BranchingStrategy) []BranchSegment {
	shape := DefaultShapeOptions()
	shape.BranchingStrategy = strategy
	return generateBranchesWithOptions(guides, noise, rng, shape)
}

func generateBranchesWithOptions(guides []Guide, noise *Perlin, rng *rand.Rand, shape ShapeOptions) []BranchSegment {
	strategy := shape.BranchingStrategy
	if strategy == BranchingSimple {
		return generateBranches(guides, noise, rng, shape)
	}
	var result []BranchSegment
	for i := range guides {
		// Resolve each guide's tree independently. Window padding may add
		// distant guides; their branch acceptance must not reorder this tree.
		var branches []BranchSegment
		g := &guides[i]
		if g.Seed != 0 {
			rng = rand.New(rand.NewSource(g.Seed ^ 0x6272616e6368))
		}
		length := g.S[len(g.S)-1]
		if strategy == BranchingLoops {
			branches = appendLoops(branches, g, guides, noise, rng, shape)
			if len(branches) > 0 {
				result = append(result, branches...)
				continue
			}
		}
		count := max(1, int(math.Round(length/.18)))
		for j := 0; j < count; j++ {
			s := length * (float64(j) + geom.Lerp(.3, .7, rng.Float64())) / float64(count)
			q, tangent, normal := g.frameAt(s)
			root := q.Add(normal.Mul(.045))
			if reliefHeight(root, guides[i:i+1], noise, nil) <= rockContourHeight {
				continue
			}
			dir := normal.Add(tangent.Mul(geom.Lerp(-.45, .45, rng.Float64()))).Norm()
			branches = growRecursiveBranch(branches, root, dir, geom.Lerp(.32, .55, rng.Float64()), shape.branchWidth(rng.Float64()), 0, noise, guides, rng, shape)
		}
		result = append(result, branches...)
	}
	return result
}

// Grow the complete parent before its children. The final taper has a short
// capped tip; most of a long limb retains enough width for terrain sampling.
// Depth and root count bound both geometry and generation work.
func growRecursiveBranch(branches []BranchSegment, root, direction geom.V, reach, width float64, depth int, noise *Perlin, guides []Guide, rng *rand.Rand, shape ShapeOptions) []BranchSegment {
	const strength = .5
	if !branchCanGrow(root, strength, width, guides) {
		return branches
	}
	phase := rng.Float64() * 1000
	start := len(branches)
	p := root
	for d := 0.0; d < reach; {
		next := math.Min(d+.012, reach)
		u, v := d/reach, next/reach
		bend := noise.Noise(p.X*6+phase, (p.Y+noise.OffsetY)*6-phase)
		heading := direction.Add(direction.Perp().Mul(.8 * bend)).Norm()
		end := p.Add(heading.Mul(next - d))
		wa := width * (1 - .72*geom.Smoothstep(.72, 1, u))
		wb := width * (1 - .72*geom.Smoothstep(.72, 1, v))
		if !branchCanGrow(end, strength, wb, guides) || !branchCanGrow(geom.LerpVector(p, end, .5), strength, (wa+wb)*.5, guides) {
			break
		}
		// Tree limbs stop before joining older limbs. Ignore the immediate
		// attachment neighborhood so a child can leave its parent's shoulder.
		blocked := false
		for _, other := range branches[:start] {
			if other.A.Sub(root).Len() < .09 || other.B.Sub(root).Len() < .09 {
				continue
			}
			if geom.SegmentDistanceSquared(p, end, other.A, other.B) < math.Pow((wb+other.WidthB)*1.15, 2) {
				blocked = true
				break
			}
		}
		if blocked {
			break
		}
		branches = append(branches, BranchSegment{A: p, B: end, WidthA: wa, WidthB: wb, LightA: strength, LightB: strength})
		p, d = end, next
	}
	end := len(branches)
	if depth >= 2 || end-start < 8 {
		return branches
	}
	for _, fraction := range []float64{.35, .65} {
		parent := branches[start+int(float64(end-start-1)*fraction)]
		side := 1.0
		if rng.Intn(2) == 0 {
			side = -1
		}
		dir := geom.Rotate(parent.B.Sub(parent.A).Norm(), side*geom.Lerp(.65, 1.05, rng.Float64()))
		branches = growRecursiveBranch(branches, parent.B, dir, reach*geom.Lerp(.5, .7, rng.Float64()), math.Max(shape.branchWidth(0), parent.WidthB*.8), depth+1, noise, guides, rng, shape)
	}
	return branches
}

// Negative points make room beside longer limbs and inside loop chambers.
// They share the segment spatial index: a zero-length segment is a radial
// point field. Max influence keeps overlapping points independent of order.
func newShapedBranchField(segs []BranchSegment, strategy BranchingStrategy) *BranchField {
	field := newBranchField(segs)
	if strategy == BranchingSimple {
		return field
	}
	var points []BranchSegment
	var last geom.V
	distance := 0.0
	for i, seg := range segs {
		if i == 0 || seg.A != last {
			distance = .04
		}
		distance += seg.B.Sub(seg.A).Len()
		last = seg.B
		if distance < .04 {
			continue
		}
		distance = 0
		width := math.Max(seg.WidthA, seg.WidthB)
		mid := geom.LerpVector(seg.A, seg.B, .5)
		normal := seg.B.Sub(seg.A).Norm().Perp()
		for _, side := range []float64{-1, 1} {
			p := mid.Add(normal.Mul(side * width * 2.2))
			points = append(points, BranchSegment{A: p, B: p, WidthA: width * 1.7, WidthB: width * 1.7, LightA: .028, LightB: .028})
		}
	}
	field.negative = newBranchField(points)
	return field
}

func branchNegativeBias(p geom.V, field *BranchField, positive, guideHeight float64, noise *Perlin) float64 {
	if field == nil || field.negative == nil {
		return 0
	}
	bias := branchBias(p, field.negative)
	if bias == 0 {
		return 0
	}
	// Close to any branch, rock remains increasingly likely to survive. Its
	// core and the guide crest are protected so a point cannot sever a limb
	// or its attachment. Coherent world-space noise varies marginal survival
	// without random per-face holes or different results in adjacent windows.
	protection := math.Max(geom.Smoothstep(.10, .38, positive), geom.Smoothstep(.020, .045, guideHeight))
	if protection == 1 {
		return 0
	}
	variation := .85 + .3*noise.Noise(p.X*23+71, (p.Y+noise.OffsetY)*23+149)
	return bias * (1 - protection) * variation
}

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

// BranchingStrategy selects how rock spurs grow from the solid side of guides.
type BranchingStrategy uint8

const (
	BranchingRecursive BranchingStrategy = iota
	BranchingSimple
	BranchingLoops
)

func (s BranchingStrategy) String() string {
	switch s {
	case BranchingSimple:
		return "simple"
	case BranchingRecursive:
		return "recursive"
	case BranchingLoops:
		return "loops"
	default:
		return fmt.Sprintf("BranchingStrategy(%d)", s)
	}
}

func ParseBranchingStrategy(name string) (BranchingStrategy, error) {
	switch name {
	case "simple":
		return BranchingSimple, nil
	case "recursive":
		return BranchingRecursive, nil
	case "loops":
		return BranchingLoops, nil
	default:
		return BranchingRecursive, fmt.Errorf("unknown branching strategy %q: use simple, recursive, or loops", name)
	}
}

// ShapeOptions selects structural branches and independent visual edge detail.
type ShapeOptions struct {
	BranchingStrategy  BranchingStrategy
	BranchMinThickness float64
	BranchMaxThickness float64
	SpikeDensity       float64
	EdgeRoughness      float64
}

func DefaultShapeOptions() ShapeOptions {
	return ShapeOptions{BranchingStrategy: BranchingRecursive, BranchMinThickness: .048, BranchMaxThickness: .08, SpikeDensity: .65, EdgeRoughness: .75}
}

// BranchThicknessBounds resolves zero endpoints to the defaults. Thickness is
// twice the nominal profile width; the soft shoulder and tip taper extend or
// narrow the actual contour.
func (s ShapeOptions) BranchThicknessBounds() (float64, float64) {
	lo, hi := s.BranchMinThickness, s.BranchMaxThickness
	if lo == 0 {
		lo = DefaultShapeOptions().BranchMinThickness
	}
	if hi == 0 {
		hi = DefaultShapeOptions().BranchMaxThickness
	}
	return lo, hi
}

func (s ShapeOptions) branchWidth(sample float64) float64 {
	lo, hi := s.BranchThicknessBounds()
	return (lo + (hi-lo)*sample) * .5
}
