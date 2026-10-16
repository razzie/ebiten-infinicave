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

// The permutation table uses dependent scalar lookups. Vectorize the fade
// polynomials and interpolation after those lookups, across FBM octaves.
func noiseBatch(n *Perlin, xs, ys, dst []float64) {
	if simd.Emulated() {
		noiseBatchScalar(n, xs, ys, dst)
		return
	}
	const batch = 8
	var xf, yf, aa, ab, ba, bb [batch]float64
	six, fifteen, ten := simd.BroadcastFloat64s(6), simd.BroadcastFloat64s(15), simd.BroadcastFloat64s(10)
	factor := simd.BroadcastFloat64s(.7071)
	fadeVector := func(t simd.Float64s) simd.Float64s {
		return t.Mul(t).Mul(t).Mul(t.Mul(t.Mul(six).Sub(fifteen)).Add(ten))
	}
	for first := 0; first < len(dst); {
		count := min(batch, len(dst)-first, factor.Len())
		for i := 0; i < count; i++ {
			x, y := xs[first+i], ys[first+i]
			x0, y0 := math.Floor(x), math.Floor(y)
			xi, yi := int(x0)&255, int(y0)&255
			xf[i], yf[i] = x-x0, y-y0
			aa[i] = grad(n.p[n.p[xi]+yi], xf[i], yf[i])
			ba[i] = grad(n.p[n.p[xi+1]+yi], xf[i]-1, yf[i])
			ab[i] = grad(n.p[n.p[xi]+yi+1], xf[i], yf[i]-1)
			bb[i] = grad(n.p[n.p[xi+1]+yi+1], xf[i]-1, yf[i]-1)
		}
		x, _ := simd.LoadFloat64sPart(xf[:count])
		y, _ := simd.LoadFloat64sPart(yf[:count])
		a, _ := simd.LoadFloat64sPart(aa[:count])
		b, _ := simd.LoadFloat64sPart(ba[:count])
		c, _ := simd.LoadFloat64sPart(ab[:count])
		d, _ := simd.LoadFloat64sPart(bb[:count])
		u, v := fadeVector(x), fadeVector(y)
		x1, x2 := a.Add(b.Sub(a).Mul(u)), c.Add(d.Sub(c).Mul(u))
		x1.Add(x2.Sub(x1).Mul(v)).Mul(factor).StorePart(dst[first : first+count])
		first += count
	}
}

func fbm(n *Perlin, p geom.V) float64 {
	if simd.Emulated() {
		return fbmScalar(n, p)
	}
	return fbmBatched(n, p)
}

func guideNearestVector(g *Guide, p geom.V) (segment int, u float64, q geom.V, best2 float64) {
	if g.projection == nil || simd.Emulated() {
		return guideNearestScalar(g, p)
	}
	c := g.projection
	best2 = math.Inf(1)
	px, py := simd.BroadcastFloat64s(p.X), simd.BroadcastFloat64s(p.Y)
	zero, one := simd.BroadcastFloat64s(0), simd.BroadcastFloat64s(1)
	var ts, xs, ys, distances [guideProjectionBlock]float64
	best2 = c.index.Nearest(p, best2, func(block int, best float64) float64 {
		if c.blocks[block].distance2(p) > best {
			return best
		}
		end := min((block+1)*guideProjectionBlock, len(c.length2))
		for first := block * guideProjectionBlock; first < end; {
			ax, count := simd.LoadFloat64sPart(c.ax[first:end])
			ay, _ := simd.LoadFloat64sPart(c.ay[first:end])
			dx, _ := simd.LoadFloat64sPart(c.dx[first:end])
			dy, _ := simd.LoadFloat64sPart(c.dy[first:end])
			l2, _ := simd.LoadFloat64sPart(c.length2[first:end])
			t := px.Sub(ax).Mul(dx).Add(py.Sub(ay).Mul(dy)).Div(l2).Min(one).Max(zero)
			x, y := ax.Add(dx.Mul(t)), ay.Add(dy.Mul(t))
			rx, ry := px.Sub(x), py.Sub(y)
			t.StorePart(ts[:count])
			x.StorePart(xs[:count])
			y.StorePart(ys[:count])
			rx.Mul(rx).Add(ry.Mul(ry)).StorePart(distances[:count])
			// Reduce in segment order with the scalar strict comparison. Ties
			// must select the first segment, including at repeated endpoints.
			for lane := 0; lane < count; lane++ {
				if c.length2[first+lane] != 0 && (distances[lane] < best || (distances[lane] == best && first+lane < segment)) {
					best, segment, u = distances[lane], first+lane, ts[lane]
					q = geom.V{X: xs[lane], Y: ys[lane]}
				}
			}
			first += count
		}
		return best
	})
	return
}
