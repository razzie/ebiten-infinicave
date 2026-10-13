package terrain

import (
	"math"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

const guideProjectionBlock = 8

type projectionBounds struct{ lo, hi geom.V }

func (b projectionBounds) distance2(p geom.V) float64 {
	dx := max(0, b.lo.X-p.X, p.X-b.hi.X)
	dy := max(0, b.lo.Y-p.Y, p.Y-b.hi.Y)
	return dx*dx + dy*dy
}

// Prepared only for the builder's owned, immutable guide window. Public guide
// inputs can still be edited and project without this cache.
type guideProjection struct {
	length2        []float64
	blocks         []projectionBounds
	ax, ay, dx, dy []float64
	index          *geom.BoundsIndex
}

func (g *Guide) prepareProjection() {
	n := len(g.Pts) - 1
	if n <= 0 {
		return
	}
	cache := &guideProjection{length2: make([]float64, n), ax: make([]float64, n), ay: make([]float64, n), dx: make([]float64, n), dy: make([]float64, n)}
	for first := 0; first < n; first += guideProjectionBlock {
		b := projectionBounds{g.Pts[first], g.Pts[first]}
		for i := first; i < min(first+guideProjectionBlock, n); i++ {
			d := g.Pts[i+1].Sub(g.Pts[i])
			cache.length2[i] = d.Len2()
			cache.ax[i], cache.ay[i], cache.dx[i], cache.dy[i] = g.Pts[i].X, g.Pts[i].Y, d.X, d.Y
			p := g.Pts[i+1]
			b.lo.X, b.lo.Y = min(b.lo.X, p.X), min(b.lo.Y, p.Y)
			b.hi.X, b.hi.Y = max(b.hi.X, p.X), max(b.hi.Y, p.Y)
		}
		// Keep pruning conservative around interpolated endpoints.
		b.lo = b.lo.Sub(geom.V{X: 1e-12, Y: 1e-12})
		b.hi = b.hi.Add(geom.V{X: 1e-12, Y: 1e-12})
		cache.blocks = append(cache.blocks, b)
	}
	g.projection = cache
	bounds := make([]geom.Bounds, len(cache.blocks))
	for i, b := range cache.blocks {
		bounds[i] = geom.Bounds{Min: b.lo, Max: b.hi}
	}
	cache.index = geom.NewBoundsIndex(bounds)
}

func guideNearestScalar(g *Guide, p geom.V) (segment int, u float64, q geom.V, best2 float64) {
	segment, u, q, best2 = guideNearestScalarLimit(g, p, math.Inf(1))
	if segment < 0 {
		segment = 0
	}
	return
}
func guideNearestScalarLimit(g *Guide, p geom.V, limit float64) (segment int, u float64, q geom.V, best2 float64) {
	segment = -1
	best2 = limit
	if g.projection != nil {
		c := g.projection
		best2 = c.index.Nearest(p, best2, func(block int, best float64) float64 {
			if c.blocks[block].distance2(p) > best {
				return best
			}
			for i := block * guideProjectionBlock; i < min((block+1)*guideProjectionBlock, len(c.length2)); i++ {
				if c.length2[i] == 0 {
					continue
				}
				d := geom.V{X: c.dx[i], Y: c.dy[i]}
				a := geom.V{X: c.ax[i], Y: c.ay[i]}
				t := geom.Clamp(p.Sub(a).Dot(d)/c.length2[i], 0, 1)
				point := a.Add(d.Mul(t))
				distance := p.Sub(point).Len2()
				if distance < best || (distance == best && (segment < 0 || i < segment)) {
					best, segment, u, q = distance, i, t, point
				}
			}
			return best
		})
		return
	}
	for i := 0; i < len(g.Pts)-1; i++ {
		a := g.Pts[i]
		d := g.Pts[i+1].Sub(a)
		l2 := d.Len2()
		if l2 == 0 {
			continue
		}
		t := geom.Clamp(p.Sub(a).Dot(d)/l2, 0, 1)
		point := a.Add(d.Mul(t))
		distance2 := p.Sub(point).Len2()
		if distance2 < best2 {
			best2, segment, u, q = distance2, i, t, point
		}
	}
	return
}
