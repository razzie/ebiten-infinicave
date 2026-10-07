package render

import (
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Faces visible from their center use a triangle fan. Deep concave guide
// cuts use ear clipping, with material rims only on the actual perimeter.
func appendCellMesh(vertices []ebiten.Vertex, indices []uint32, poly []geom.V, center geom.V, clr color.NRGBA, surface geom.V3) ([]ebiten.Vertex, []uint32) {
	if len(poly) < 3 {
		return vertices, indices
	}
	radius := 0.0
	for _, p := range poly {
		radius = math.Max(radius, p.Sub(center).Len())
	}
	radius = math.Max(radius, .001)
	tilt := geom.V{X: surface.X, Y: surface.Y}
	type facetTriangle struct {
		center, a, b geom.V
		boundary     bool
	}
	var facets []facetTriangle
	fan := true
	for i, a := range poly {
		if geom.Cross(poly[(i+1)%len(poly)].Sub(a), center.Sub(a)) < -1e-15 {
			fan = false
			break
		}
	}
	if fan {
		for i, a := range poly {
			facets = append(facets, facetTriangle{center, a, poly[(i+1)%len(poly)], true})
		}
	} else {
		for _, tri := range geom.Triangulate(poly) {
			mid := poly[tri[0]].Add(poly[tri[1]]).Add(poly[tri[2]]).Mul(1.0 / 3)
			for i, a := range tri {
				b := tri[(i+1)%3]
				facets = append(facets, facetTriangle{mid, poly[a], poly[b], (a+1)%len(poly) == b})
			}
		}
	}
	for _, triangle := range facets {
		a, b := triangle.a, triangle.b
		normal := b.Sub(a).Perp().Norm()
		if triangle.center.Sub(a).Dot(normal) < 0 {
			normal = normal.Mul(-1)
		}
		first := uint32(len(vertices))
		for _, p := range []geom.V{triangle.center, a, b} {
			edgeDistance := .002 // Internal triangulation edges have no rim.
			if triangle.boundary {
				edgeDistance = math.Max(0, p.Sub(a).Dot(normal))
			}
			vertices = append(vertices, ebiten.Vertex{
				DstX: float32(p.X * RasterPixelsPerUnit), DstY: float32((p.Y - terrain.GenerationMinY) * RasterPixelsPerUnit),
				SrcX: float32(p.X * RasterPixelsPerUnit), SrcY: float32((p.Y - terrain.GenerationMinY) * RasterPixelsPerUnit),
				ColorR: float32(clr.R) / 255, ColorG: float32(clr.G) / 255,
				ColorB: float32(clr.B) / 255, ColorA: float32(clr.A) / 255,
				Custom0: float32(edgeDistance * RasterPixelsPerUnit),
				Custom1: float32(p.Sub(center).Dot(tilt) / radius),
				Custom2: float32(surface.X),
				Custom3: float32(surface.Y),
			})
		}
		indices = append(indices, first, first+1, first+2)
	}
	return vertices, indices
}
