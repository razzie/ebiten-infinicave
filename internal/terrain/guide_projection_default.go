//go:build !goexperiment.simd

package terrain

import "github.com/razzie/ebiten-infinicave/internal/geom"

func guideNearest(g *Guide, p geom.V) (int, float64, geom.V, float64) {
	return guideNearestScalar(g, p)
}
