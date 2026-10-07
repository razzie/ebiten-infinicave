package render

import (
	"image/color"
	"math"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestConcaveCellMeshStaysInsideFace(t *testing.T) {
	poly := []geom.V{{X: 0.1, Y: 0.1}, {X: 0.3, Y: 0.1}, {X: 0.3, Y: 0.3}, {X: 0.24, Y: 0.3}, {X: 0.24, Y: 0.16}, {X: 0.16, Y: 0.16}, {X: 0.16, Y: 0.3}, {X: 0.1, Y: 0.3}}
	vertices, indices := appendCellMesh(nil, nil, poly, geom.PolygonCenter(poly), color.NRGBA{R: 200, A: 255}, geom.V3{Z: 1})
	point := func(v ebiten.Vertex) geom.V {
		return geom.V{X: float64(v.DstX) / RasterPixelsPerUnit, Y: float64(v.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY}
	}
	area := 0.0
	for i := 0; i < len(indices); i += 3 {
		a, b, c := point(vertices[indices[i]]), point(vertices[indices[i+1]]), point(vertices[indices[i+2]])
		area += geom.Cross(b.Sub(a), c.Sub(a)) * .5
		if !geom.InsidePolygon(a.Add(b).Add(c).Mul(1.0/3), poly) {
			t.Fatal("mesh triangle paints across the guide")
		}
	}
	if math.Abs(area-geom.PolygonArea(poly)) > 2e-8 {
		t.Fatalf("mesh area %v differs from face area %v", area, geom.PolygonArea(poly))
	}
}

func TestCellMeshHasOneContinuousShadingPlane(t *testing.T) {
	polys := [][]geom.V{
		// A ridged crest creates short edges and narrow fan triangles.
		{{X: 0, Y: 0.01}, {X: 0.02, Y: 0.008}, {X: 0.023, Y: 0.009}, {X: 0.045, Y: 0}, {X: 0.08, Y: 0}, {X: 0.08, Y: 0.06}, {X: 0, Y: 0.06}},
		// Concave merged cells use ear clipping instead of a single fan.
		{{X: 0.1, Y: 0.1}, {X: 0.3, Y: 0.1}, {X: 0.3, Y: 0.3}, {X: 0.24, Y: 0.3}, {X: 0.24, Y: 0.16}, {X: 0.16, Y: 0.16}, {X: 0.16, Y: 0.3}, {X: 0.1, Y: 0.3}},
	}
	for _, poly := range polys {
		vertices, _ := appendCellMesh(nil, nil, poly, geom.PolygonCenter(poly), color.NRGBA{R: 200, A: 255}, geom.V3{X: .6, Y: -.6, Z: .5})
		type shading struct{ tilt, facet float32 }
		seen := make(map[[2]float32]shading)
		for _, v := range vertices {
			key := [2]float32{v.DstX, v.DstY}
			value := shading{v.Custom1, v.Custom2}
			if previous, ok := seen[key]; ok && previous != value {
				t.Fatal("internal triangulation introduces a tonal seam within one rock")
			}
			seen[key] = value
		}
	}
}
