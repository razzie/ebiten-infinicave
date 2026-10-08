//go:build goexperiment.simd

package terrain

import (
	"math"
	"simd"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// Use separate Mul/Add operations: fused arithmetic could change contour and
// growth decisions. Coordinates come from the same scalar-generated samples.
func fillFloat64(dst []float64, value float64) {
	if simd.Emulated() || len(dst) < (simd.Float64s{}).Len() {
		fillFloat64Scalar(dst, value)
		return
	}
	v := simd.BroadcastFloat64s(value)
	n := v.Len()
	i := 0
	for ; i+n <= len(dst); i += n {
		v.Store(dst[i:])
	}
	fillFloat64Scalar(dst[i:], value)
}

func rasterDepthSpan(dst []float64, first int, py float64, c RockCell) {
	if simd.Emulated() || len(dst) < (simd.Float64s{}).Len() {
		rasterDepthSpanScalar(dst, first, py, c)
		return
	}
	cz := simd.BroadcastFloat64s(c.Z)
	cx := simd.BroadcastFloat64s(c.Center.X)
	nx := simd.BroadcastFloat64s(c.Normal.X)
	nz := simd.BroadcastFloat64s(c.Normal.Z)
	dy := simd.BroadcastFloat64s((py - c.Center.Y) * c.Normal.Y)
	lo, hi := c.Z-.004, c.Z+.004
	if c.Raised {
		lo, hi = math.Max(.001, c.Z-.028), c.Z+.028
	}
	vlo, vhi := simd.BroadcastFloat64s(lo), simd.BroadcastFloat64s(hi)
	n := cz.Len()
	i := 0
	for ; i+n <= len(dst); i += n {
		z := cz
		if c.Normal.Z >= .05 {
			x := simd.LoadFloat64s(depthSampleX[first+i:])
			z = cz.Sub(x.Sub(cx).Mul(nx).Add(dy).Div(nz)).Min(vhi).Max(vlo)
		}
		simd.LoadFloat64s(dst[i:]).Max(z).Store(dst[i:])
	}
	rasterDepthSpanScalar(dst[i:], first+i, py, c)
}

func segmentDistanceSpan(dst []float64, first int, py float64, a, d geom.V, scale float64, divide, clearance bool) {
	if simd.Emulated() || len(dst) < (simd.Float64s{}).Len() {
		segmentDistanceSpanScalar(dst, first, py, a, d, scale, divide, clearance)
		return
	}
	ax, ay := simd.BroadcastFloat64s(a.X), simd.BroadcastFloat64s(a.Y)
	dx, dy := simd.BroadcastFloat64s(d.X), simd.BroadcastFloat64s(d.Y)
	y := simd.BroadcastFloat64s(py)
	ydot := simd.BroadcastFloat64s((py - a.Y) * d.Y)
	s := simd.BroadcastFloat64s(scale)
	zero, one := simd.BroadcastFloat64s(0), simd.BroadcastFloat64s(1)
	reach := simd.BroadcastFloat64s(foregroundVineGuideClearance + vineBorderRange)
	margin := simd.BroadcastFloat64s(foregroundVineGuideClearance)
	n := s.Len()
	i := 0
	for ; i+n <= len(dst); i += n {
		x := simd.LoadFloat64s(vineSampleX[first+i:])
		t := x.Sub(ax).Mul(dx).Add(ydot)
		if divide {
			t = t.Div(s)
		} else {
			t = t.Mul(s)
		}
		t = t.Min(one).Max(zero)
		rx, ry := x.Sub(ax.Add(dx.Mul(t))), y.Sub(ay.Add(dy.Mul(t)))
		distance := rx.Mul(rx).Add(ry.Mul(ry)).Sqrt()
		old := simd.LoadFloat64s(dst[i:])
		if clearance {
			updated := old.Min(distance.Sub(margin).Max(zero))
			updated = updated.IfElse(distance.Less(reach), old)
			updated = updated.IfElse(old.NotEqual(zero), old)
			updated.Store(dst[i:])
		} else {
			old.Min(distance).Store(dst[i:])
		}
	}
	segmentDistanceSpanScalar(dst[i:], first+i, py, a, d, scale, divide, clearance)
}

func blendVineTone(dst []float64, tone, alpha float64) {
	if simd.Emulated() || len(dst) < (simd.Float64s{}).Len() {
		blendVineToneScalar(dst, tone, alpha)
		return
	}
	t, a := simd.BroadcastFloat64s(tone), simd.BroadcastFloat64s(alpha)
	n := t.Len()
	i := 0
	for ; i+n <= len(dst); i += n {
		v := simd.LoadFloat64s(dst[i:])
		v.Add(t.Sub(v).Mul(a)).Store(dst[i:])
	}
	blendVineToneScalar(dst[i:], tone, alpha)
}

func classifyVineTones(f *VineTerrain, tones []float64) {
	if simd.Emulated() {
		classifyVineTonesScalar(f, tones)
		return
	}
	low, high := simd.BroadcastFloat64s(vineVoidTone), simd.BroadcastFloat64s(vineLightTone)
	step, inf := simd.BroadcastFloat64s(vineFieldStep), simd.BroadcastFloat64s(math.Inf(1))
	n := low.Len()
	for y := 0; y < vineFieldHeight; y++ {
		top := simd.BroadcastFloat64s(float64(min(y+1, vineFieldHeight-y)))
		x := 0
		for ; x+n <= vineFieldWidth; x += n {
			i := y*vineFieldWidth + x
			t := simd.LoadFloat64s(tones[i:])
			valid := t.Greater(low).And(t.Less(high))
			v := simd.LoadFloat64s(vineSideClearance[x:]).Min(top).Mul(step)
			v.IfElse(valid, simd.LoadFloat64s(f.clearance[i:])).Store(f.clearance[i:])
			if !f.onForeground {
				simd.LoadFloat64s(f.unsupported[i:]).IfElse(valid, inf).Store(f.unsupported[i:])
			}
		}
		for ; x < vineFieldWidth; x++ {
			i := y*vineFieldWidth + x
			if tones[i] > vineVoidTone && tones[i] < vineLightTone {
				f.clearance[i] = min(vineSideClearance[x], float64(min(y+1, vineFieldHeight-y))) * vineFieldStep
			} else if !f.onForeground {
				f.unsupported[i] = math.Inf(1)
			}
		}
	}
}
