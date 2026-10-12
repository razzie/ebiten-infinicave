package terrain

import (
	"math"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

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
