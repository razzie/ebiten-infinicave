//go:build goexperiment.simd

package terrain

import (
	"math"
	"simd"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func guideNearest(g *Guide, p geom.V) (segment int, u float64, q geom.V, best2 float64) {
	if g.projection == nil || simd.Emulated() {
		return guideNearestScalar(g, p)
	}
	c := g.projection
	best2 = math.Inf(1)
	px, py := simd.BroadcastFloat64s(p.X), simd.BroadcastFloat64s(p.Y)
	zero, one := simd.BroadcastFloat64s(0), simd.BroadcastFloat64s(1)
	var ts, xs, ys, distances [guideProjectionBlock]float64
	for block, bounds := range c.blocks {
		if bounds.distance2(p) >= best2 {
			continue
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
				if c.length2[first+lane] != 0 && distances[lane] < best2 {
					best2, segment, u = distances[lane], first+lane, ts[lane]
					q = geom.V{X: xs[lane], Y: ys[lane]}
				}
			}
			first += count
		}
	}
	return
}
