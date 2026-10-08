//go:build goexperiment.simd

package render

import (
	"image"
	"simd"

	"github.com/hajimehoshi/ebiten/v2"
)

func transformVertices(vertices []ebiten.Vertex, scale float64, offset image.Point) {
	if simd.Emulated() || len(vertices) < (simd.Float64s{}).Len() {
		transformVerticesScalar(vertices, scale, offset)
		return
	}
	var xs, ys [8]float64
	s := simd.BroadcastFloat64s(scale)
	for first := 0; first < len(vertices); {
		count := min(len(xs), s.Len(), len(vertices)-first)
		for i := 0; i < count; i++ {
			xs[i], ys[i] = float64(vertices[first+i].DstX), float64(vertices[first+i].DstY)
		}
		x, _ := simd.LoadFloat64sPart(xs[:count])
		y, _ := simd.LoadFloat64sPart(ys[:count])
		x.Mul(s).StorePart(xs[:count])
		y.Mul(s).StorePart(ys[:count])
		for i := 0; i < count; i++ {
			// Preserve the float32 rounding before adding the raster origin.
			vertices[first+i].DstX = float32(xs[i]) + float32(offset.X)
			vertices[first+i].DstY = float32(ys[i]) + float32(offset.Y)
		}
		first += count
	}
}
