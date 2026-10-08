//go:build !goexperiment.simd

package render

import (
	"github.com/hajimehoshi/ebiten/v2"
	"image"
)

func transformVertices(vertices []ebiten.Vertex, scale float64, offset image.Point) {
	transformVerticesScalar(vertices, scale, offset)
}
