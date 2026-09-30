package main

import (
	"math"
	"math/rand"
)

// BranchSegment carries a tapering ribbon of light through the existing cells.
// Branches affect whole faces, leaving the guide's paired crest seeds intact.
type BranchSegment struct {
	A, B           V
	WidthA, WidthB float64
	LightA, LightB float64
}

func generateBranches(guides []Guide, noise *Perlin, rng *rand.Rand) []BranchSegment {
	var branches []BranchSegment
	for i := range guides {
		g := &guides[i]
		length := g.S[len(g.S)-1]
		count := int(math.Max(1, math.Round(length/115)))
		for j := 0; j < count; j++ {
			// Stratified roots leave gaps between the spreading shoulders.
			s := length * (float64(j) + lerp(.25, .75, rng.Float64())) / float64(count)
			q, tangent, normal := g.frameAt(s)
			root := q.Add(normal.Mul(guideSpacing * .65))
			direction := normal.Add(tangent.Mul(lerp(-.65, .65, rng.Float64()))).Norm()
			reach := lerp(155, 285, rng.Float64())
			width := lerp(32, 49, rng.Float64())
			phase := rng.Float64() * 1000
			forkAt := lerp(.38, .60, rng.Float64())
			forked := false
			p := root
			for d := 0.0; d < reach; {
				next := math.Min(d+12, reach)
				u, v := d/reach, next/reach
				// A coherent lateral drift makes connected fingers instead of
				// isolated patches from thresholding a second noise field.
				bend := noise.Noise(p.X*.009+phase, p.Y*.009-phase)
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
		next := math.Min(d+12, reach)
		u, v := d/reach, next/reach
		bend := noise.Noise(p.X*.012+phase, p.Y*.012-phase)
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

func branchBias(p V, branches []BranchSegment) float64 {
	light := 0.0
	for _, b := range branches {
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
