package render

import (
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestCarveRimsFollowCutBoundariesAndDiagnosticViews(t *testing.T) {
	data := terrain.SectionData{Foreground: terrain.RockGrid{terrainRect(.1, .1, .8, .8)}}
	cut, err := terrain.CutFromHole(terrain.Hole{Shape: terrain.HoleCircle, Center: geom.V{X: .5, Y: -.5}, Radius: .12})
	if err != nil {
		t.Fatal(err)
	}
	h, _, changed := cut.Geometry(0, terrain.PrepareTerrainGeometry(data, 0), 0)
	if !changed {
		t.Fatal("blast did not change terrain")
	}
	mesh := PrepareShapeRims(h, ViewShaded)
	if len(mesh.Indices) == 0 {
		t.Fatal("cut boundary has no local rim")
	}
	checkMesh(t, mesh)
	for _, v := range mesh.Vertices {
		p := geom.V{X: float64(v.DstX) / RasterPixelsPerUnit, Y: float64(v.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY}
		if distance := p.Sub(geom.V{X: .5, Y: .5}).Len(); distance < .10 || distance > .14 {
			t.Fatalf("rim escaped cut: %v", p)
		}
	}
	if len(PrepareShapeRims(h, ViewClay).Indices) == 0 || len(PrepareShapeRims(h, ViewNormals).Indices) != 0 {
		t.Fatal("rim appeared in diagnostic view or disappeared from clay")
	}
}
