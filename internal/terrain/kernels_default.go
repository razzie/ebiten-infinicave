//go:build !goexperiment.simd

package terrain

import (
	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func fillFloat64(dst []float64, value float64) { fillFloat64Scalar(dst, value) }
func rasterDepthSpan(dst []float64, first int, py float64, c RockCell) {
	rasterDepthSpanScalar(dst, first, py, c)
}
func segmentDistanceSpan(dst []float64, first int, py float64, a, d geom.V, scale float64, divide, clearance bool) {
	segmentDistanceSpanScalar(dst, first, py, a, d, scale, divide, clearance)
}
func blendVineTone(dst []float64, tone, alpha float64)  { blendVineToneScalar(dst, tone, alpha) }
func classifyVineTones(f *VineTerrain, tones []float64) { classifyVineTonesScalar(f, tones) }

func fbm(n *Perlin, p geom.V) float64 { return fbmScalar(n, p) }

func noiseBatch(n *Perlin, xs, ys, dst []float64) { noiseBatchScalar(n, xs, ys, dst) }

func guideNearestVector(g *Guide, p geom.V) (int, float64, geom.V, float64) {
	return guideNearestScalar(g, p)
}
