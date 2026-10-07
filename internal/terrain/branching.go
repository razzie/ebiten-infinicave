package terrain

import (
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

func generateBranches(guides []Guide, noise *Perlin, rng *rand.Rand) []BranchSegment {
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
			width := geom.Lerp(0.026, 0.04, rng.Float64())
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
					branches = appendBranchFork(branches, end, forkDir, reach*geom.Lerp(.35, .55, rng.Float64()), width*.55, .425*(1-geom.Smoothstep(.12, 1, v)), phase, noise, guides)
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
