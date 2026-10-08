//go:build goexperiment.simd

package terrain

import (
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"math"
	"simd"
)

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
