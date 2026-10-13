package terrain

import (
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"testing"
)

func BenchmarkGuideProjection(b *testing.B) {
	g := SplineGuide([]geom.V{{X: .1, Y: .6}, {X: .6, Y: .2}, {X: .9, Y: .8}, {X: .4, Y: .9}, {X: .1, Y: .6}}, 1)
	g.prepareProjection()
	points := []geom.V{{X: .1, Y: .55}, {X: .5, Y: .3}, {X: .8, Y: .7}, {X: .2, Y: .2}, {X: .7, Y: .2}}
	b.Run("indexed", func(b *testing.B) {
		for i := 0; b.Loop(); i++ {
			guideNearest(&g, points[i%len(points)])
		}
	})
	b.Run("vector", func(b *testing.B) {
		for i := 0; b.Loop(); i++ {
			guideNearestVector(&g, points[i%len(points)])
		}
	})
}
