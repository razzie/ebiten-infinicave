package render

import (
	"reflect"
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
	topology := terrain.CarvedTopology(h.Grid, h.Cuts, h.Top)
	vs, is := AppendCarveRims(nil, nil, topology, ViewShaded)
	if len(is) == 0 {
		t.Fatal("blast did not expose any crater walls/rim")
	}
	checkMesh(t, TriangleMesh{Vertices: vs, Indices: is})
	for _, v := range vs {
		p := geom.V{X: float64(v.DstX) / RasterPixelsPerUnit, Y: float64(v.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY}
		if distance := p.Sub(geom.V{X: .5, Y: .5}).Len(); distance < .10 || distance > .14 {
			t.Fatalf("decorative rim escaped cut boundary: %v", p)
		}
	}
	shaded := PrepareGridWithTopology(nil, ViewShaded, topology)
	normals := PrepareGridWithTopology(nil, ViewNormals, topology)
	withoutCuts := *topology
	withoutCuts.Cuts = nil
	normalsNoRim := PrepareGridWithTopology(nil, ViewNormals, &withoutCuts)
	if len(shaded.Faces.Indices) <= len(normals.Faces.Indices) || !reflect.DeepEqual(normals, normalsNoRim) {
		t.Fatal("rim should appear in shaded/clay views and leave diagnostic geometry alone")
	}
}
