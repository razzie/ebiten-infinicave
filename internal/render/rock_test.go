package render

import (
	"context"
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

func TestBackgroundBoundaryCellAddsFacetsWithoutChangingFootprint(t *testing.T) {
	poly := []geom.V{{X: .1, Y: .1}, {X: .15, Y: .09}, {X: .2, Y: .1}, {X: .19, Y: .15}, {X: .2, Y: .2}, {X: .15, Y: .19}, {X: .1, Y: .2}}
	cell := terrainRock(poly)
	cell.Raised, cell.Normal = false, geom.V3{Z: 1}
	vertices, indices := appendBackgroundCellMesh(nil, nil, cell, color.NRGBA{R: 30, G: 30, B: 30, A: 255})
	normals := make(map[[2]float32]bool)
	area := 0.0
	for i := 0; i < len(indices); i += 3 {
		var points [3]geom.V
		for k := range points {
			v := vertices[indices[i+k]]
			points[k] = geom.V{X: float64(v.DstX) / RasterPixelsPerUnit, Y: float64(v.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY}
			normals[[2]float32{v.Custom2, v.Custom3}] = true
		}
		mid := points[0].Add(points[1]).Add(points[2]).Mul(1.0 / 3)
		if !geom.InsidePolygon(mid, poly) {
			t.Fatal("extra background facet extends outside the jagged cell")
		}
		area += geom.PolygonArea(points[:])
	}
	if len(normals) < 2 || math.Abs(area-geom.PolygonArea(poly)) > 1e-8 {
		t.Fatal("background facets did not add distinct planes covering the original cell")
	}
}

func TestBackgroundInteriorKeepsOneShadingPlane(t *testing.T) {
	var grid terrain.RockGrid
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			cell := terrainRect(.2+float64(x)*.1, .2+float64(y)*.1, .1, .1)
			cell.Raised, cell.Normal = false, geom.V3{Z: 1}
			cell.Color = color.NRGBA{R: 30, G: 30, B: 30, A: 255}
			grid = append(grid, cell)
		}
	}
	mesh := prepareGrid(grid, ViewClay).Faces
	planes := [2]map[[2]float32]bool{make(map[[2]float32]bool), make(map[[2]float32]bool)}
	for i := 0; i < len(mesh.Indices); i += 3 {
		mid := geom.V{}
		for k := range 3 {
			v := mesh.Vertices[mesh.Indices[i+k]]
			mid = mid.Add(geom.V{X: float64(v.DstX) / RasterPixelsPerUnit, Y: float64(v.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY})
		}
		mid = mid.Mul(1.0 / 3)
		for slot, cell := range []int{4, 0} {
			if geom.InsidePolygon(mid, grid[cell].Polygon) {
				v := mesh.Vertices[mesh.Indices[i]]
				planes[slot][[2]float32{v.Custom2, v.Custom3}] = true
			}
		}
	}
	if len(planes[0]) != 1 || len(planes[1]) != 2 {
		t.Fatalf("interior/boundary shading planes: %d/%d, want 1/2", len(planes[0]), len(planes[1]))
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

func TestFoldRockEdgesPreservesTopsAndStructuralGrid(t *testing.T) {
	points := make(map[int]geom.V)
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		var grid terrain.RockGrid
		for i := range 20 {
			c := terrainRect(.1+float64(i%5)*.15, .2+float64(i/5)*.15, .08, .08)
			world := geom.V3{Z: 1}
			if i == 0 {
				world = (geom.V3{Y: -.8, Z: .6}).Norm()
			}
			c.Normal = world
			xy := terrain.InternalPoint(mode, geom.V{X: world.X, Y: world.Y})
			c.Normal.X, c.Normal.Y = xy.X, xy.Y
			c.Center = terrain.InternalPoint(mode, c.Center)
			terrain.MapPoints(c.Polygon, func(p geom.V) geom.V { return terrain.InternalPoint(mode, p) })
			grid = append(grid, terrain.WithRockOrientation(c, mode))
		}
		boundary := terrain.ExposedRockEdges(grid)
		shape := terrain.ShapeOptions{EdgeRoughness: 1}
		folded, edges, splits := foldRockEdges(grid, boundary, shape)
		if len(splits) == 0 || len(splits) >= len(grid)-1 {
			t.Fatalf("%v: expected occasional folds, got %d", mode, len(splits))
		}
		if !reflect.DeepEqual(folded[0], grid[0]) {
			t.Fatalf("%v: an upward-facing cell acquired a fold", mode)
		}
		if len(edges) != len(boundary)+len(splits) {
			t.Fatal("a folded air edge was not divided into exactly two")
		}
		for i, split := range splits {
			world := terrain.RockMaterialPoint(grid[i], split.point)
			if mode == terrain.Vertical {
				points[i] = world
			} else if previous, ok := points[i]; !ok || previous.Sub(world).Len() > 1e-9 {
				t.Fatal("rotating the scene changed its fold choices")
			}
			if len(grid[i].Polygon) != 4 || len(folded[i].Polygon) != 5 || !geom.InsidePolygon(split.point, grid[i].Polygon) {
				t.Fatal("fold changed structural geometry or left the original rock")
			}
			outward := split.edge.B.Sub(split.edge.A).Norm().Perp().Mul(-1)
			if terrain.WorldPoint(mode, outward).Y < -.1 {
				t.Fatal("an upward-facing edge acquired a fold")
			}
			if geom.PolygonArea(folded[i].Polygon) >= geom.PolygonArea(grid[i].Polygon) {
				t.Fatal("fold did not break the exposed silhouette")
			}
		}
		again, againEdges, againSplits := foldRockEdges(grid, boundary, shape)
		if !reflect.DeepEqual(folded, again) || !reflect.DeepEqual(edges, againEdges) || !reflect.DeepEqual(splits, againSplits) {
			t.Fatal("rebuilding changed the folds")
		}
		plain, _, none := foldRockEdges(grid, boundary, terrain.ShapeOptions{})
		if !reflect.DeepEqual(plain, grid) || len(none) != 0 {
			t.Fatal("disabled roughness changed the faces")
		}
	}
}

func TestSplitRockCellMeshPreservesConcaveCoverageAndShadingPlanes(t *testing.T) {
	poly := []geom.V{{X: .1, Y: .1}, {X: .3, Y: .1}, {X: .3, Y: .3}, {X: .24, Y: .3}, {X: .24, Y: .16}, {X: .16, Y: .16}, {X: .16, Y: .3}, {X: .1, Y: .3}}
	c := terrainRock(poly)
	c.Normal = (geom.V3{X: .4, Y: .2, Z: 1}).Norm()
	split := rockFaceSplit{point: geom.V{X: .2, Y: .1}, normal: geom.V{X: 1}}
	vertices, indices := appendSplitRockCellMesh(nil, nil, c, color.NRGBA{R: 120, G: 110, B: 100, A: 255}, split)
	point := func(v ebiten.Vertex) geom.V {
		return geom.V{X: float64(v.DstX) / RasterPixelsPerUnit, Y: float64(v.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY}
	}
	area := 0.0
	shades := make(map[float32]bool)
	planes := make(map[[4]float32]float32)
	for _, v := range vertices {
		shades[v.ColorR] = true
		key := [4]float32{v.DstX, v.DstY, v.Custom2, v.Custom3}
		if previous, ok := planes[key]; ok && previous != v.Custom1 {
			t.Fatal("internal triangles introduced a shading seam within a half")
		}
		planes[key] = v.Custom1
	}
	for i := 0; i < len(indices); i += 3 {
		a, b, d := point(vertices[indices[i]]), point(vertices[indices[i+1]]), point(vertices[indices[i+2]])
		area += geom.Cross(b.Sub(a), d.Sub(a)) * .5
		if !geom.InsidePolygon(a.Add(b).Add(d).Mul(1.0/3), poly) {
			t.Fatal("split filled the concave air gap")
		}
	}
	if math.Abs(area-geom.PolygonArea(poly)) > 2e-8 || len(shades) != 2 {
		t.Fatalf("split coverage or shading is incorrect: area=%g shades=%d", area, len(shades))
	}
}

func TestRockFoldReplacesSampledCurveWithBroadAngularEdges(t *testing.T) {
	var grid terrain.RockGrid
	for i := range 12 {
		dx := float64(i) * .25
		poly := []geom.V{{X: .1, Y: .1}, {X: .3, Y: .1}, {X: .3, Y: .24}, {X: .28, Y: .26}, {X: .25, Y: .275}, {X: .2, Y: .28}, {X: .15, Y: .275}, {X: .12, Y: .26}, {X: .1, Y: .24}}
		for j := range poly {
			poly[j].X += dx
		}
		c := terrainRock(poly)
		c.Normal = (geom.V3{Y: .4, Z: 1}).Norm()
		grid = append(grid, c)
	}
	folded, edges, splits := foldRockEdges(grid, terrain.ExposedRockEdges(grid), terrain.ShapeOptions{EdgeRoughness: 1})
	curves := 0
	for i, split := range splits {
		if split.count < 2 {
			continue
		}
		curves++
		if len(folded[i].Polygon) != len(grid[i].Polygon)-split.count+2 {
			t.Fatal("fold kept the small contour segments instead of replacing them")
		}
		length := split.edge.B.Sub(split.edge.A).Len()
		inset := math.Sqrt(geom.SegmentDistanceSquared(split.point, split.point, split.edge.A, split.edge.B))
		if inset < length*.08 {
			t.Fatalf("fold is too shallow to visibly break the curve: %g along %g", inset, length)
		}
		count := 0
		for _, e := range edges {
			if e.Cell == i && (e.A.Sub(split.point).Len() < 1e-9 || e.B.Sub(split.point).Len() < 1e-9) {
				count++
			}
		}
		if count != 2 {
			t.Fatal("curved contour did not become two angular edges")
		}
	}
	if curves < len(grid)/2 {
		t.Fatalf("too few curved silhouettes were broken up: %d/%d", curves, len(grid))
	}
}

func TestSplitRockCellMeshKeepsTrianglesTouchingFold(t *testing.T) {
	poly := []geom.V{{X: .4, Y: .4}, {X: .6, Y: .4}, {X: .6, Y: .6}, {X: .5, Y: .52}, {X: .4, Y: .6}}
	c := terrainRock(poly)
	c.Normal = geom.V3{Z: 1}
	split := rockFaceSplit{point: poly[3], normal: geom.V{X: 1}}
	checkSplitRockCoverage(t, c, split)
}

func checkSplitRockCoverage(t *testing.T, c terrain.RockCell, split rockFaceSplit) {
	t.Helper()
	vertices, indices := appendSplitRockCellMesh(nil, nil, c, color.NRGBA{R: 100, G: 90, B: 80, A: 255}, split)
	checkMesh(t, TriangleMesh{Vertices: vertices, Indices: indices})
	point := func(v ebiten.Vertex) geom.V {
		return geom.V{X: float64(v.DstX) / RasterPixelsPerUnit, Y: float64(v.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY}
	}
	area := 0.0
	for i := 0; i < len(indices); i += 3 {
		a, b, d := point(vertices[indices[i]]), point(vertices[indices[i+1]]), point(vertices[indices[i+2]])
		area += geom.Cross(b.Sub(a), d.Sub(a)) * .5
	}
	if math.Abs(area-geom.PolygonArea(c.Polygon)) > 2e-8 {
		t.Fatalf("fold lost polygons at %v: rendered area %g, expected %g", c.Center, area, geom.PolygonArea(c.Polygon))
	}
}

func TestSplitRockCellsKeepGeneratedCoverage(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		for _, seed := range []int64{1, 42} {
			builder := terrain.NewSectionBuilder(seed, nil, mode)
			builder.Shape, builder.ForceGuidesUp = terrain.DefaultShapeOptions(), true
			data := builder.Begin(context.Background(), 0).Data()
			topology := terrain.PrepareRockAppearance(data.ForegroundTopology)
			grid, boundary, splits := foldRockEdges(topology.Appearance, topology.Boundary, data.Shape)
			grid, _, _ = visualBoundaryGrid(grid, boundary, RockGrowthOptions{Shape: data.Shape, Guides: data.Guides}, false, ViewShaded)
			if len(splits) == 0 {
				t.Fatalf("%v seed %d: no split cells to check", mode, seed)
			}
			for i, split := range splits {
				checkSplitRockCoverage(t, grid[i], split)
			}
		}
	}
}

func TestRockEdgeFoldsMatchOverlappingSections(t *testing.T) {
	for _, mode := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		builder := terrain.NewSectionBuilder(42, nil, mode)
		builder.Shape, builder.ForceGuidesUp = terrain.DefaultShapeOptions(), true
		type foldedFace struct {
			polygon []geom.V
			folded  bool
		}
		var previous map[[2]int64]foldedFace
		shared := 0
		for id := range int64(2) {
			data := builder.Begin(context.Background(), id).Data()
			topology := terrain.PrepareRockAppearance(data.ForegroundTopology)
			grid, _, splits := foldRockEdges(topology.Appearance, topology.Boundary, data.Shape)
			faces := make(map[[2]int64]foldedFace)
			for i, c := range grid {
				y := c.Center.Y + terrain.SectionTop(id)
				if y < -1.75 || y > -.25 {
					continue
				}
				center := terrain.RockMaterialPoint(c, c.Center)
				key := [2]int64{int64(math.Round(center.X * 1e8)), int64(math.Round(center.Y * 1e8))}
				poly := append([]geom.V(nil), c.Polygon...)
				for j, p := range poly {
					poly[j] = terrain.RockMaterialPoint(c, p)
				}
				_, folded := splits[i]
				faces[key] = foldedFace{poly, folded}
				if previous != nil {
					before, ok := previous[key]
					if !ok || before.folded != folded || len(before.polygon) != len(poly) {
						t.Fatalf("%v: neighboring section changed fold selection", mode)
					}
					for j, p := range poly {
						if p.Sub(before.polygon[j]).Len() > 1e-9 {
							t.Fatalf("%v: neighboring section changed folded geometry", mode)
						}
					}
					if folded {
						shared++
					}
				}
			}
			previous = faces
		}
		if shared == 0 {
			t.Fatalf("%v: fixture has no shared folds", mode)
		}
	}
}
