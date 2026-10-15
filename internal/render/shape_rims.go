package render

import (
	"image/color"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// PrepareShapeRims consumes tagged current cut boundaries; it does not scan
// historical cuts or change source rock lighting and surface decorations.
func PrepareShapeRims(geometry *terrain.Geometry, view View) TriangleMesh {
	return PrepareShapeRimsInBounds(geometry, view, geom.Bounds{Min: geom.V{X: -1, Y: terrain.GenerationMinY - 1}, Max: geom.V{X: 2, Y: 3}})
}

func PrepareShapeRimsInBounds(geometry *terrain.Geometry, view View, bounds geom.Bounds) TriangleMesh {
	m := TriangleMesh{MaterialFrame: ReferenceMaterialFrame()}
	if geometry == nil || (view != ViewShaded && view != ViewClay) {
		return m
	}
	for _, e := range geometry.CutEdgesInBounds(bounds) {
		outward := e.B.Sub(e.A).Norm().Perp().Mul(-1)
		mid := geom.LerpVector(e.A, e.B, .5)
		key := terrain.CollisionVertexKey(mid.Add(geom.V{Y: geometry.Top}))
		r := terrain.SiteRandom(0x637261746572, key[0], key[1], 0)
		depth := .003 + .003*r
		width := .0015 + .002*r
		shadow := color.NRGBA{R: 30, G: 23, B: 17, A: 240}
		lip := color.NRGBA{R: uint8(102 + 40*r), G: uint8(85 + 35*r), B: uint8(62 + 25*r), A: 255}
		if view == ViewClay {
			shadow = color.NRGBA{R: 25, G: 25, B: 25, A: 240}
			lip = color.NRGBA{R: 100, G: 100, B: 100, A: 255}
		}
		m.Vertices, m.Indices = appendRockQuad(m.Vertices, m.Indices, [4]geom.V{e.A, e.B, e.B.Add(outward.Mul(depth)), e.A.Add(outward.Mul(depth))}, shadow)
		m.Vertices, m.Indices = appendRockQuad(m.Vertices, m.Indices, [4]geom.V{e.A, e.B, e.B.Sub(outward.Mul(width)), e.A.Sub(outward.Mul(width))}, lip)
	}
	return m
}
