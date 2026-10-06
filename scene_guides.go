package infinicave

import (
	"math"
	"math/rand"
)

const (
	guideClearance         = 155.0 // room for a rock flank and an open approach to the next lip
	guideCoverageRadius    = 115.0
	guideCoverageTarget    = .68
	guidePlacementAttempts = 600
)

type guidePocket struct {
	center V
	radius float64
}

type guideLayout struct {
	pockets []guidePocket
	probes  []V
}

func newGuideLayout(rng *rand.Rand) guideLayout {
	layout := guideLayout{}
	for n := 1 + rng.Intn(2); n > 0; n-- {
		layout.pockets = append(layout.pockets, guidePocket{
			V{lerp(170, generationWidth-170, rng.Float64()), lerp(170, generationWidth-170, rng.Float64())},
			lerp(85, 135, rng.Float64()),
		})
	}
	// Jittered probes measure coverage without arranging guides in rows.
	const side = 12
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			layout.probes = append(layout.probes, V{
				(float64(x) + lerp(.2, .8, rng.Float64())) * generationWidth / side,
				(float64(y) + lerp(.2, .8, rng.Float64())) * generationWidth / side,
			})
		}
	}
	return layout
}

func generateGuides(rng *rand.Rand) []Guide {
	var guides []Guide
	for top := 0; top < generationHeight; top += generationWidth {
		var obstacles []Guide
		for _, g := range guides {
			obstacles = append(obstacles, shiftedGuide(g, -float64(top)))
		}
		layout := newGuideLayout(rng)
		section := layout.fill(rng, nil, obstacles, 0)
		for _, g := range section {
			guides = append(guides, shiftedGuide(g, float64(top)))
		}
	}
	return guides
}

func generateGuideSection(rng *rand.Rand) []Guide {
	return newGuideLayout(rng).fill(rng, nil, nil, 0)
}

// Grow a fresh curve with independently varying length, heading, curvature,
// and knot spacing. Persistent turns make broad hooks; changing turns make
// uneven ledges and S-bends. There are no reusable knot templates.
func randomGuide(rng *rand.Rand, center V, crowded bool) Guide {
	length := lerp(230, 620, rng.Float64())
	if crowded {
		length = lerp(150, 360, rng.Float64())
	}
	angle := lerp(-.8, .8, rng.Float64())
	if rng.Float64() < .22 {
		angle = lerp(-math.Pi/2, math.Pi/2, rng.Float64())
	}
	turn := lerp(-1.3, 1.3, rng.Float64())
	knots := []V{{}}
	for s := 0.0; s < length; {
		step := math.Min(lerp(45, 90, rng.Float64()), length-s)
		angle += turn * step / 100
		knots = append(knots, knots[len(knots)-1].Add(V{math.Cos(angle), math.Sin(angle)}.Mul(step)))
		turn = clamp(turn*.75+lerp(-.55, .55, rng.Float64()), -1.4, 1.4)
		s += step
	}
	lo, hi := knots[0], knots[0]
	for _, p := range knots {
		lo = V{math.Min(lo.X, p.X), math.Min(lo.Y, p.Y)}
		hi = V{math.Max(hi.X, p.X), math.Max(hi.Y, p.Y)}
	}
	offset := center.Sub(lerpV(lo, hi, .5))
	for i := range knots {
		knots[i] = knots[i].Add(offset)
	}
	return ridgedGuide(splineGuide(knots, 1), rng.Int63())
}

func (layout guideLayout) fill(rng *rand.Rand, guides, obstacles []Guide, margin float64) []Guide {
	distances := make([]float64, len(layout.probes))
	for i := range distances {
		distances[i] = math.Inf(1)
	}
	update := func(g *Guide) {
		for i, p := range layout.probes {
			if g.distanceBound2(p) < distances[i]*distances[i] {
				distances[i] = math.Min(distances[i], g.project(p).Dist)
			}
		}
	}
	for i := range guides {
		update(&guides[i])
	}
	for attempt := 0; attempt < guidePlacementAttempts; attempt++ {
		covered := 0
		for _, d := range distances {
			if d <= guideCoverageRadius {
				covered++
			}
		}
		if float64(covered)/float64(len(distances)) >= guideCoverageTarget {
			break
		}
		// Favor underused parts of the scene, retaining enough randomness to
		// avoid a regular packing pattern. Smaller curves can fill late gaps.
		anchor := rng.Intn(len(distances))
		for j := 0; j < 5; j++ {
			i := rng.Intn(len(distances))
			if distances[i] > distances[anchor] {
				anchor = i
			}
		}
		center := layout.probes[anchor].Add(V{lerp(-40, 40, rng.Float64()), lerp(-40, 40, rng.Float64())})
		g := randomGuide(rng, center, attempt > guidePlacementAttempts/3)
		if g.Min.X < foregroundScreenInset || g.Max.X > generationWidth-foregroundScreenInset || g.Min.Y < margin || g.Max.Y > generationWidth-margin {
			continue
		}
		valid := guideSelfClear(g)
		for _, pocket := range layout.pockets {
			if g.project(pocket.center).Dist < pocket.radius {
				valid = false
				break
			}
		}
		if !valid || !guideClearOf(g, guides) || !guideClearOf(g, obstacles) {
			continue
		}
		guides = append(guides, g)
		update(&g)
	}
	return guides
}

func shiftedGuide(g Guide, offset float64) Guide {
	g.Pts = append([]V(nil), g.Pts...)
	g.translateY(offset)
	return g
}

// Segment distances also catch crossings between vertices and close parallel
// edges; checking only knots or bounding boxes misses both.
func guideSegmentsDistance2(a, b, c, d V) float64 {
	ab, cd := b.Sub(a), d.Sub(c)
	den := cross(ab, cd)
	if math.Abs(den) > 1e-9 {
		u, v := cross(c.Sub(a), cd)/den, cross(c.Sub(a), ab)/den
		if u >= 0 && u <= 1 && v >= 0 && v <= 1 {
			return 0
		}
	}
	pointDistance := func(p, q, r V) float64 {
		delta := r.Sub(q)
		u := 0.0
		if delta.Len2() > 0 {
			u = clamp(p.Sub(q).Dot(delta)/delta.Len2(), 0, 1)
		}
		return p.Sub(q.Add(delta.Mul(u))).Len2()
	}
	return min(pointDistance(a, c, d), pointDistance(b, c, d), pointDistance(c, a, b), pointDistance(d, a, b))
}

func guidesTooClose(a, b Guide) bool {
	dx := max(0, a.Min.X-b.Max.X, b.Min.X-a.Max.X)
	dy := max(0, a.Min.Y-b.Max.Y, b.Min.Y-a.Max.Y)
	if dx*dx+dy*dy >= guideClearance*guideClearance {
		return false
	}
	for i := 1; i < len(a.Pts); i++ {
		for j := 1; j < len(b.Pts); j++ {
			if guideSegmentsDistance2(a.Pts[i-1], a.Pts[i], b.Pts[j-1], b.Pts[j]) < guideClearance*guideClearance {
				return true
			}
		}
	}
	return false
}

func guideClearOf(g Guide, others []Guide) bool {
	for _, other := range others {
		if guidesTooClose(g, other) {
			return false
		}
	}
	return true
}

func guideSelfClear(g Guide) bool {
	for i := 1; i < len(g.Pts); i++ {
		for j := i + 2; j < len(g.Pts); j++ {
			distance2 := guideSegmentsDistance2(g.Pts[i-1], g.Pts[i], g.Pts[j-1], g.Pts[j])
			if distance2 < 1e-12 {
				return false
			}
			// Adjacent parts of a continuous curve are necessarily close.
			// Only distant parts along its arc must leave a usable opening.
			if g.S[j-1]-g.S[i] < guideClearance*1.6 {
				continue
			}
			if distance2 < guideClearance*guideClearance {
				return false
			}
		}
	}
	return true
}

// Resolve boundary conflicts against deterministic neighbor proposals, then
// refill holes. Refills stay half a clearance inside their owner, so adjacent
// refills cannot collide. Decisions never depend on which window loads first.
func spacedWorldGuideSection(seed, owner int64, proposals map[int64][]Guide) []Guide {
	var guides, obstacles []Guide
	priority := uint64(sectionSeed(seed^0x7370616365, owner))
	for neighbor := owner - 1; neighbor <= owner+1; neighbor += 2 {
		for _, g := range proposals[neighbor] {
			obstacles = append(obstacles, shiftedGuide(g, float64(owner-neighbor)*generationWidth))
		}
	}
	for _, g := range proposals[owner] {
		valid := true
		for neighbor := owner - 1; neighbor <= owner+1; neighbor += 2 {
			otherPriority := uint64(sectionSeed(seed^0x7370616365, neighbor))
			if otherPriority < priority || (otherPriority == priority && neighbor < owner) {
				continue
			}
			for _, other := range proposals[neighbor] {
				if guidesTooClose(g, shiftedGuide(other, float64(owner-neighbor)*generationWidth)) {
					valid = false
					break
				}
			}
		}
		if valid {
			guides = append(guides, g)
		}
	}
	layout := newGuideLayout(rand.New(rand.NewSource(sectionSeed(seed, owner))))
	return layout.fill(rand.New(rand.NewSource(sectionSeed(seed^0x66696c6c, owner))), guides, obstacles, guideClearance/2)
}
