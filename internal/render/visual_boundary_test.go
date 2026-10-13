package render

import (
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestVisualBoundaryCutsCoverageAndRetainsSmoothShape(t *testing.T) {
	c := terrainRect(.2, .3, .6, .4)
	grid := terrain.RockGrid{c}
	before := append([]geom.V(nil), c.Polygon...)
	var edges []terrain.RockEdge
	for j, a := range c.Polygon {
		edges = append(edges, terrain.RockEdge{A: a, B: c.Polygon[(j+1)%len(c.Polygon)]})
	}
	growth := RockGrowthOptions{Shape: terrain.ShapeOptions{EdgeRoughness: 1, SpikeDensity: .4}}
	visual, boundary, teeth := visualBoundaryGrid(grid, edges, growth, false, ViewShaded)
	if !reflect.DeepEqual(grid[0].Polygon, before) || !reflect.DeepEqual(grid[0].Normal, visual[0].Normal) {
		t.Fatal("visual roughness modified structure or lighting")
	}
	if geom.PolygonArea(visual[0].Polygon) >= geom.PolygonArea(c.Polygon) || len(boundary) <= len(edges) {
		t.Fatal("inward chips did not remove coverage")
	}
	if len(teeth) == 0 {
		t.Fatal("missing supported outward teeth")
	}
	for _, a := range teeth {
		for _, p := range a.Anchors {
			if !geom.InsidePolygon(p, c.Polygon) {
				t.Fatal("tooth support lies outside structural rock")
			}
		}
		cut, err := terrain.CutFromHole(terrain.Hole{Shape: terrain.HoleCircle, Center: a.Anchors[0], Radius: .002})
		if err != nil {
			t.Fatal(err)
		}
		if !a.Hit(cut, 0) {
			t.Fatal("removing a tooth support leaves detached trim")
		}
		checkMesh(t, a.Mesh)
	}
	if !reflect.DeepEqual(visual[0].Polygon, func() []geom.V {
		g, _, _ := visualBoundaryGrid(grid, edges, growth, false, ViewShaded)
		return g[0].Polygon
	}()) {
		t.Fatal("visual boundary is nondeterministic")
	}
}

func TestVisualBoundaryAttenuatesGuideCrests(t *testing.T) {
	c := terrainRect(.2, .3, .6, .4)
	edge := terrain.RockEdge{A: c.Polygon[0], B: c.Polygon[1]}
	growth := RockGrowthOptions{Shape: terrain.ShapeOptions{EdgeRoughness: 1}}
	plain, _, _ := visualBoundaryGrid(terrain.RockGrid{c}, []terrain.RockEdge{edge}, growth, false, ViewShaded)
	growth.Guides = []terrain.Guide{{Pts: []geom.V{edge.A, edge.B}}}
	crest, _, _ := visualBoundaryGrid(terrain.RockGrid{c}, []terrain.RockEdge{edge}, growth, false, ViewShaded)
	if geom.PolygonArea(c.Polygon)-geom.PolygonArea(crest[0].Polygon) >= geom.PolygonArea(c.Polygon)-geom.PolygonArea(plain[0].Polygon) {
		t.Fatal("guide crest did not attenuate chips")
	}
}
