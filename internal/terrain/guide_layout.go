package terrain

import (
	"math"
	"math/rand"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

const (
	// Keep the whole exposed lip above the rock contour during edge fading.
	// Placing a guide closer can erase most of a full-sized formation and
	// leave just a few disconnected crests.
	foregroundGuideInset   = foregroundEdgeFadeWidth / 2
	guideClearance         = .155 // room for a rock flank and an open approach to the next lip
	guideCoverageRadius    = .115
	guideCoverageTarget    = .68
	guidePlacementAttempts = 600
)

type guidePocket struct {
	center geom.V
	radius float64
}

type guideLayout struct {
	orientation Orientation
	pockets     []guidePocket
	probes      []geom.V
}

func newGuideLayout(rng *rand.Rand, orientation ...Orientation) guideLayout {
	layout := guideLayout{orientation: OptionalOrientation(orientation)}
	for n := 1 + rng.Intn(2); n > 0; n-- {
		layout.pockets = append(layout.pockets, guidePocket{
			geom.V{X: geom.Lerp(0.17, generationWidth-0.17, rng.Float64()), Y: geom.Lerp(0.17, generationWidth-0.17, rng.Float64())},
			geom.Lerp(0.085, 0.135, rng.Float64()),
		})
	}
	// Jittered probes measure coverage without arranging guides in rows.
	const side = 12
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			layout.probes = append(layout.probes, geom.V{
				X: (float64(x) + geom.Lerp(.2, .8, rng.Float64())) * generationWidth / side,
				Y: (float64(y) + geom.Lerp(.2, .8, rng.Float64())) * generationWidth / side,
			})
		}
	}
	return layout
}

func generateGuides(rng *rand.Rand) []Guide {
	var guides []Guide
	for top := GenerationMinY; top < generationMaxY; top += generationWidth {
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

func generateGuideSection(rng *rand.Rand, orientation ...Orientation) []Guide {
	return newGuideLayout(rng, orientation...).fill(rng, nil, nil, 0)
}

// Grow a fresh curve with independently varying length, heading, curvature,
// and knot spacing. Persistent turns make broad hooks; changing turns make
// uneven ledges and S-bends. There are no reusable knot templates.
func randomGuide(rng *rand.Rand, center geom.V, crowded bool, orientation ...Orientation) Guide {
	length := geom.Lerp(0.23, 0.62, rng.Float64())
	if crowded {
		length = geom.Lerp(0.15, 0.36, rng.Float64())
	}
	angle := geom.Lerp(-.8, .8, rng.Float64())
	preferHorizontal := rng.Float64() >= .22
	if !preferHorizontal {
		angle = geom.Lerp(-math.Pi/2, math.Pi/2, rng.Float64())
	}
	if OptionalOrientation(orientation) == Horizontal {
		angle -= math.Pi / 2 // World-horizontal ledges in the section frame.
	}
	turn := geom.Lerp(-1.3, 1.3, rng.Float64())
	knots := []geom.V{{}}
	for s := 0.0; s < length; {
		step := math.Min(geom.Lerp(0.045, 0.09, rng.Float64()), length-s)
		// Absorb a short remainder into this segment. A tiny final knot
		// after a normal-sized one makes Catmull-Rom overshoot and fold the
		// exposed lip backward, creating small disconnected tip fragments.
		if length-s-step < .045 {
			step = length - s
		}
		angle += turn * step / 0.1
		knots = append(knots, knots[len(knots)-1].Add(geom.V{X: math.Cos(angle), Y: math.Sin(angle)}.Mul(step)))
		turn = geom.Clamp(turn*.75+geom.Lerp(-.55, .55, rng.Float64()), -1.4, 1.4)
		s += step
	}
	// Curvature can erase the initial heading bias. Classify the completed
	// curve in world axes, keeping most proposals horizontal without removing
	// the minority of steep ledges and hooks.
	if preferHorizontal {
		var dx, dy float64
		mode := OptionalOrientation(orientation)
		for i := 1; i < len(knots); i++ {
			d := WorldPoint(mode, knots[i].Sub(knots[i-1]))
			dx += math.Abs(d.X)
			dy += math.Abs(d.Y)
		}
		if dy > dx {
			for i, p := range knots {
				knots[i] = p.Perp()
			}
		}
	}
	lo, hi := knots[0], knots[0]
	for _, p := range knots {
		lo = geom.V{X: math.Min(lo.X, p.X), Y: math.Min(lo.Y, p.Y)}
		hi = geom.V{X: math.Max(hi.X, p.X), Y: math.Max(hi.Y, p.Y)}
	}
	offset := center.Sub(geom.LerpVector(lo, hi, .5))
	for i := range knots {
		knots[i] = knots[i].Add(offset)
	}
	return RidgedGuide(SplineGuide(knots, 1), rng.Int63())
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
		center := layout.probes[anchor].Add(geom.V{X: geom.Lerp(-0.04, 0.04, rng.Float64()), Y: geom.Lerp(-0.04, 0.04, rng.Float64())})
		g := randomGuide(rng, center, attempt > guidePlacementAttempts/3, layout.orientation)
		if g.Min.X < foregroundGuideInset || g.Max.X > generationWidth-foregroundGuideInset || g.Min.Y < margin || g.Max.Y > generationWidth-margin {
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
	g.Pts = append([]geom.V(nil), g.Pts...)
	g.translateY(offset)
	return g
}

func guidesTooClose(a, b Guide) bool {
	dx := max(0, a.Min.X-b.Max.X, b.Min.X-a.Max.X)
	dy := max(0, a.Min.Y-b.Max.Y, b.Min.Y-a.Max.Y)
	if dx*dx+dy*dy >= guideClearance*guideClearance {
		return false
	}
	for i := 1; i < len(a.Pts); i++ {
		for j := 1; j < len(b.Pts); j++ {
			if geom.SegmentDistanceSquared(a.Pts[i-1], a.Pts[i], b.Pts[j-1], b.Pts[j]) < guideClearance*guideClearance {
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
			distance2 := geom.SegmentDistanceSquared(g.Pts[i-1], g.Pts[i], g.Pts[j-1], g.Pts[j])
			if distance2 < 1e-18 {
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
func spacedWorldGuideSection(seed, owner int64, proposals map[int64][]Guide, orientation ...Orientation) []Guide {
	var guides, obstacles []Guide
	priority := uint64(SectionSeed(seed^0x7370616365, owner))
	for neighbor := owner - 1; neighbor <= owner+1; neighbor += 2 {
		for _, g := range proposals[neighbor] {
			obstacles = append(obstacles, shiftedGuide(g, float64(owner-neighbor)*generationWidth))
		}
	}
	for _, g := range proposals[owner] {
		valid := true
		for neighbor := owner - 1; neighbor <= owner+1; neighbor += 2 {
			otherPriority := uint64(SectionSeed(seed^0x7370616365, neighbor))
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
	layout := newGuideLayout(rand.New(rand.NewSource(SectionSeed(seed, owner))), orientation...)
	return layout.fill(rand.New(rand.NewSource(SectionSeed(seed^0x66696c6c, owner))), guides, obstacles, guideClearance/2)
}
