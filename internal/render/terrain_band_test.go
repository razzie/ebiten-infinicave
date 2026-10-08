package render

import (
	"image/color"
	"reflect"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Compare the actual ordered primitives touching the destination, including
// material coordinates. Removed primitives must be entirely outside it.
func bandTriangles(mesh TriangleMesh, pixels int) [][3]ebiten.Vertex {
	var triangles [][3]ebiten.Vertex
	for i := 0; i < len(mesh.Indices); i += 3 {
		tri := [3]ebiten.Vertex{mesh.Vertices[mesh.Indices[i]], mesh.Vertices[mesh.Indices[i+1]], mesh.Vertices[mesh.Indices[i+2]]}
		lo, hi := tri[0].DstY, tri[0].DstY
		for _, v := range tri[1:] {
			lo = min(lo, v.DstY)
			hi = max(hi, v.DstY)
		}
		if hi >= 0 && lo <= float32(pixels) {
			triangles = append(triangles, tri)
		}
	}
	return triangles
}

func TestOwnedTerrainKeepsVisiblePrimitives(t *testing.T) {
	for _, raised := range []bool{false, true} {
		var grid terrain.RockGrid
		for _, y := range []float64{-.8, -.001, .2, .999, 1.7} {
			poly := []geom.V{{X: .2, Y: y}, {X: .3, Y: y}, {X: .3, Y: y + .1}, {X: .2, Y: y + .1}}
			grid = append(grid, terrain.RockCell{Center: geom.PolygonCenter(poly), Polygon: poly, Normal: geom.V3{Z: 1}, Color: color.NRGBA{R: 80, G: 60, B: 40, A: 255}, Raised: raised, Z: .05, Shadow: 1, Ambient: 1})
		}
		for _, view := range []View{ViewShaded, ViewClay, ViewNormals} {
			full := prepareGrid(grid, view)
			owned := PrepareOwnedGridWithTopology(grid, view, nil)
			if len(owned.Faces.Indices) >= len(full.Faces.Indices) {
				t.Fatal("off-band terrain was not culled")
			}
			for _, pixels := range []int{1, 2, 7, 501, 1000, 1920} {
				a, b := ScaleGridMesh(full, pixels, 0), ScaleGridMesh(owned, pixels, 0)
				if !reflect.DeepEqual(bandTriangles(a.Faces, pixels), bandTriangles(b.Faces, pixels)) {
					t.Fatalf("%v %s %d: visible faces changed", raised, view, pixels)
				}
				var ao, bo [][3]ebiten.Vertex
				for _, mesh := range a.Outlines {
					ao = append(ao, bandTriangles(mesh, pixels)...)
				}
				for _, mesh := range b.Outlines {
					bo = append(bo, bandTriangles(mesh, pixels)...)
				}
				if !reflect.DeepEqual(ao, bo) {
					t.Fatalf("%v %s %d: visible outlines changed", raised, view, pixels)
				}
			}
		}
	}
}
