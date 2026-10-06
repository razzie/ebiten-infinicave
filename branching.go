package infinicave

import (
	"math"
	"math/rand"
)

// BranchSegment carries a tapering raised spur through the existing cells.
// LightA/B are the historical strength values, now used as relief amplitude.
type BranchSegment struct {
	A, B           V
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
			s := length * (float64(j) + lerp(.25, .75, rng.Float64())) / float64(count)
			q, tangent, normal := g.frameAt(s)
			// The ridge flank reaches 0.07–0.14 scene units from the guide; spurs must emerge from its foot.
			root := q.Add(normal.Mul(lerp(0.07, 0.095, rng.Float64())))
			direction := normal.Add(tangent.Mul(lerp(-.65, .65, rng.Float64()))).Norm()
			reach := lerp(0.17, 0.3, rng.Float64())
			width := lerp(0.026, 0.04, rng.Float64())
			phase := rng.Float64() * 1000 // Dimensionless noise phase, independent of scene scale.
			forkAt := lerp(.38, .60, rng.Float64())
			forked := false
			p := root
			for d := 0.0; d < reach; {
				next := math.Min(d+0.012, reach)
				u, v := d/reach, next/reach
				// A coherent lateral drift makes connected fingers instead of
				// isolated patches from thresholding a second noise field.
				bend := noise.Noise(p.X*9+phase, (p.Y+noise.OffsetY)*9-phase)
				heading := direction.Add(direction.Perp().Mul(bend * 1.5)).Norm()
				end := p.Add(heading.Mul(next - d))
				branches = append(branches, BranchSegment{
					A: p, B: end,
					WidthA: width * lerp(1, .22, u), WidthB: width * lerp(1, .22, v),
					LightA: .50 * (1 - smoothstep(.12, 1, u)), LightB: .50 * (1 - smoothstep(.12, 1, v)),
				})
				if !forked && v >= forkAt {
					forked = true
					side := 1.0
					if rng.Intn(2) == 0 {
						side = -1
					}
					forkDir := heading.Add(heading.Perp().Mul(side * .95)).Norm()
					branches = appendBranchFork(branches, end, forkDir, reach*lerp(.35, .55, rng.Float64()), width*.55, .425*(1-smoothstep(.12, 1, v)), phase, noise)
				}
				p, d = end, next
			}
		}
	}
	return branches
}

func appendBranchFork(branches []BranchSegment, p, direction V, reach, width, light, phase float64, noise *Perlin) []BranchSegment {
	for d := 0.0; d < reach; {
		next := math.Min(d+0.012, reach)
		u, v := d/reach, next/reach
		bend := noise.Noise(p.X*12+phase, (p.Y+noise.OffsetY)*12-phase)
		heading := direction.Add(direction.Perp().Mul(bend)).Norm()
		end := p.Add(heading.Mul(next - d))
		branches = append(branches, BranchSegment{
			A: p, B: end,
			WidthA: width * lerp(1, .25, u), WidthB: width * lerp(1, .25, v),
			LightA: light * (1 - smoothstep(0, 1, u)), LightB: light * (1 - smoothstep(0, 1, v)),
		})
		p, d = end, next
	}
	return branches
}

const branchCell = .032

// BranchField buckets segments by padded bounds; a spur contributes nothing
// outside its support, and the max in branchBias is order independent.
type BranchField struct {
	segs       []BranchSegment
	lo, hi     []V
	cols, rows int
	origin     V
	cells      [][]int32
}

func newBranchField(segs []BranchSegment) *BranchField {
	f := &BranchField{segs: segs, lo: make([]V, len(segs)), hi: make([]V, len(segs))}
	if len(segs) == 0 {
		return f
	}
	min, max := V{math.Inf(1), math.Inf(1)}, V{math.Inf(-1), math.Inf(-1)}
	for i, b := range segs {
		pad := math.Max(b.WidthA, b.WidthB) * 1.8
		f.lo[i] = V{math.Min(b.A.X, b.B.X) - pad, math.Min(b.A.Y, b.B.Y) - pad}
		f.hi[i] = V{math.Max(b.A.X, b.B.X) + pad, math.Max(b.A.Y, b.B.Y) + pad}
		min = V{math.Min(min.X, f.lo[i].X), math.Min(min.Y, f.lo[i].Y)}
		max = V{math.Max(max.X, f.hi[i].X), math.Max(max.Y, f.hi[i].Y)}
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

func branchBias(p V, field *BranchField) float64 {
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
			u = clamp(p.Sub(b.A).Dot(delta)/delta.Len2(), 0, 1)
		}
		distance := p.Sub(lerpV(b.A, b.B, u)).Len()
		width := lerp(b.WidthA, b.WidthB, u)
		strength := lerp(b.LightA, b.LightB, u)
		// Max preserves dark gaps and avoids overexposing intersecting limbs.
		light = math.Max(light, strength*(1-smoothstep(width*.15, width*1.8, distance)))
	}
	return light
}
