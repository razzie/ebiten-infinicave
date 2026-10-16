package render

import (
	"image/color"
	"math"
	"reflect"
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

func TestRockCrevicesDistinguishValleysFromFlatAndConvexJoins(t *testing.T) {
	grid := terrain.RockGrid{terrainRect(.4, .4, .04, .04), terrainRect(.44, .4, .04, .02)}
	for i := range grid {
		grid[i].Normal, grid[i].Z = geom.V3{Z: 1}, .04
		grid[i].Color = color.NRGBA{R: 120, G: 110, B: 100, A: 255}
	}
	if seams := rockCrevices(grid); len(seams) != 0 {
		t.Fatal("coplanar facets acquired an outline")
	}
	grid[0].Normal = (geom.V3{X: .8, Z: 1}).Norm()
	grid[1].Normal = (geom.V3{X: -.8, Z: 1}).Norm()
	seams := rockCrevices(grid)
	if len(seams) != 1 || seams[0].alpha == 0 {
		t.Fatal("recessed partial join has no contact crack")
	}
	if math.Abs(seams[0].a.X-.44) > 1e-10 || math.Abs(seams[0].b.X-.44) > 1e-10 ||
		seams[0].a.Y <= .4 || seams[0].b.Y >= .42 {
		t.Fatal("crack extends outside the shared edge or into exposed endpoints")
	}
	grid[0].Normal.X *= -1
	grid[1].Normal.X *= -1
	if seams := rockCrevices(grid); len(seams) != 0 {
		t.Fatal("convex ridge acquired a recessed crack")
	}
	grid[0].Normal, grid[1].Normal = geom.V3{Z: 1}, geom.V3{Z: 1}
	grid[1].Z += .02
	if seams := rockCrevices(grid); len(seams) != 1 {
		t.Fatal("depth step between flat faces lost its contact crack")
	}
}

func TestBackgroundCrevicesAreSelectiveAndRespectBlackPockets(t *testing.T) {
	a, b := terrainRect(.4, .4, .04, .04), terrainRect(.44, .4, .04, .04)
	for _, c := range []*terrain.RockCell{&a, &b} {
		c.Raised, c.Z, c.Normal = false, -.008, geom.V3{Z: 1}
		c.Color = color.NRGBA{R: 25, G: 24, B: 22, A: 255}
	}
	mid := geom.V{X: .44, Y: .42}
	if rockCreviceStrength(a, b, mid) != 0 {
		t.Fatal("flat background facets gained a crevice")
	}
	a.Normal, b.Normal = (geom.V3{X: .15, Z: 1}).Norm(), (geom.V3{X: -.15, Z: 1}).Norm()
	if strength := rockCreviceStrength(a, b, mid); strength <= 0 || strength > .65 {
		t.Fatal("shallow background valley has no subdued crevice")
	}
	b.Color = color.NRGBA{A: 255}
	if rockCreviceStrength(a, b, mid) != 0 {
		t.Fatal("crevice shading enters a black background pocket")
	}
}

func TestHorizontalNormalDiagnostic(t *testing.T) {
	up := terrain.InternalPoint(terrain.Horizontal, geom.V{X: 0, Y: -1})
	// The normal diagnostic must report world axes too.
	cell := terrain.WithRockOrientation(terrain.RockCell{Normal: geom.V3{X: up.X, Y: up.Y, Z: 0}}, terrain.Horizontal)
	if clr := rockViewColor(cell, ViewNormals); clr.G != 0 || clr.R != 127 {
		t.Fatalf("horizontal normal diagnostic: %+v", clr)
	}
}

func TestExposedRockEdgesCancelPartialNeighbors(t *testing.T) {
	grid := terrain.RockGrid{
		{Center: geom.V{X: 0.11, Y: 0.11}, Polygon: []geom.V{{X: 0.1, Y: 0.1}, {X: 0.12, Y: 0.1}, {X: 0.12, Y: 0.12}, {X: 0.1, Y: 0.12}}},
		{Center: geom.V{X: 0.125, Y: 0.105}, Polygon: []geom.V{{X: 0.12, Y: 0.1}, {X: 0.13, Y: 0.1}, {X: 0.13, Y: 0.11}, {X: 0.12, Y: 0.11}}},
	}
	edges := terrain.ExposedRockEdges(grid)
	length := 0.0
	for _, e := range edges {
		length += e.B.Sub(e.A).Len()
		if e.A.X == .120 && e.B.X == .120 && (e.A.Y+e.B.Y)*.5 < .110 {
			t.Fatal("shared partial edge incorrectly exposes a side wall")
		}
	}
	if math.Abs(length-.100) > 1e-9 {
		t.Fatalf("wrong exposed perimeter: %v", length)
	}
	for i := range grid {
		grid[i].Raised, grid[i].Z, grid[i].Normal = true, .060, geom.V3{Z: 1}
		grid[i].Shadow, grid[i].Ambient = 1, 1
		grid[i].Color = color.NRGBA{R: 100, G: 100, B: 100, A: 255}
	}
	vertices, indices := appendRockWalls(nil, nil, grid, edges, ViewClay)
	if len(indices) == 0 {
		t.Fatal("raised silhouette has no side geometry")
	}
	for _, v := range vertices {
		if math.IsNaN(float64(v.DstX)) || v.ColorR != v.ColorG || v.ColorG != v.ColorB {
			t.Fatal("invalid or non-neutral wall in clay view")
		}
	}
}

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
