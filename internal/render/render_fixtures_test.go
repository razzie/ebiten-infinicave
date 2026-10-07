package render

// Helpers for renderer mesh construction and geometry assertions.

import (
	"image/color"
	"math"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func terrainRock(poly []geom.V) terrain.RockCell {
	return terrain.RockCell{Polygon: poly, Center: geom.PolygonCenter(poly), Raised: true, Color: color.NRGBA{A: 255}}
}

func terrainRect(x, y, width, height float64) terrain.RockCell {
	return terrainRock([]geom.V{{X: x, Y: y}, {X: x + width, Y: y}, {X: x + width, Y: y + height}, {X: x, Y: y + height}})
}

func resolutionTestMesh(id int64) SectionMesh {
	data := terrain.SectionData{ID: id,
		Background: terrain.RockGrid{{Center: geom.V{X: .5, Y: .5}, Polygon: []geom.V{{X: 0, Y: -1}, {X: 1, Y: -1}, {X: 1, Y: 2}, {X: 0, Y: 2}}, Color: color.NRGBA{R: 40, A: 255}, Normal: geom.V3{Z: 1}}},
		Foreground: terrain.RockGrid{{Center: geom.V{X: .5, Y: .5}, Polygon: []geom.V{{X: .2, Y: .2}, {X: .8, Y: .2}, {X: .8, Y: .8}, {X: .2, Y: .8}}, Color: color.NRGBA{R: 100, A: 255}, Normal: geom.V3{Z: 1}, Raised: true}},
	}
	mesh := PrepareSection(data, ViewClay)
	mesh.Geometry = terrain.PrepareTerrainGeometry(data, 0)
	FinishSectionMesh(&mesh)
	return mesh
}

func checkMesh(t *testing.T, mesh TriangleMesh) {
	t.Helper()
	if len(mesh.Indices)%3 != 0 {
		t.Fatal("mesh has incomplete triangles")
	}
	for _, index := range mesh.Indices {
		if int(index) >= len(mesh.Vertices) {
			t.Fatal("mesh index exceeds vertex count")
		}
	}
	for _, v := range mesh.Vertices {
		if math.IsNaN(float64(v.DstX)) || math.IsNaN(float64(v.DstY)) || math.IsInf(float64(v.DstX), 0) || math.IsInf(float64(v.DstY), 0) {
			t.Fatal("mesh has nonfinite geometry")
		}
	}
}
