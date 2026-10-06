package infinicave

import (
	"image/color"
	"math"
	"math/rand"
	"testing"
)

func TestReliefProfileHasBevelCrestAndBroadFlank(t *testing.T) {
	const width, height, bevel = .180, .072, .016
	if ridgeProfile(-0.001, width, height, bevel) != 0 || ridgeProfile(width, width, height, bevel) != 0 {
		t.Fatal("relief extends outside its footprint")
	}
	if ridgeProfile(0, width, height, bevel) <= 0 || ridgeProfile(bevel, width, height, bevel) != height {
		t.Fatal("exposed lip has no thickness or crest")
	}
	for sample := 1; sample < 180; sample++ {
		d := float64(sample) * .001
		change := ridgeProfile(d, width, height, bevel) - ridgeProfile(d-0.001, width, height, bevel)
		if (d <= bevel && change <= 0) || (d > bevel && change >= 0) {
			t.Fatalf("relief changes slope at the wrong place: d=%v", d)
		}
	}
}

func TestReliefHeightAndSiteDeformationUseWorldCoordinates(t *testing.T) {
	a := []Guide{splineGuide([]V{{0.1, 0.4}, {0.5, 0.35}, {0.9, 0.4}}, 1)}
	b := []Guide{splineGuide([]V{{0.1, 0.4}, {0.5, 0.35}, {0.9, 0.4}}, 1)}
	b[0].translateY(1)
	n1, n2 := NewPerlin(rand.New(rand.NewSource(42))), NewPerlin(rand.New(rand.NewSource(42)))
	n2.OffsetY = -1
	for y := 0.35; y < 0.7; y += 0.013 {
		p, q := V{0.45, y}, V{0.45, y + 1}
		if math.Abs(reliefHeight(p, a, n1, nil)-reliefHeight(q, b, n2, nil)) > 1e-9 {
			t.Fatal("relief changes between generation windows")
		}
		x, z := reliefSeeds([]V{p}, a)[0], reliefSeeds([]V{q}, b)[0]
		z.Y -= 1
		if x.Sub(z).Len() > 1e-9 {
			t.Fatal("facet deformation changes between generation windows")
		}
	}
}

func TestReliefFadesBeforeHorizontalScreenInset(t *testing.T) {
	guides := []Guide{splineGuide([]V{{-0.1, 0.5}, {0.9, 0.5}}, 1)}
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	atEdge := reliefHeight(V{0, 0.5}, guides, noise, nil)
	atInset := reliefHeight(V{foregroundScreenInset, 0.5}, guides, noise, nil)
	inset := reliefHeight(V{0.09, 0.5}, guides, noise, nil)
	if atEdge != 0 || atInset >= rockContourHeight {
		t.Fatalf("foreground relief survives at the screen edge: edge=%v inset=%v", atEdge, atInset)
	}
	if inset <= rockContourHeight {
		t.Fatalf("edge fade erased interior foreground relief: %v", inset)
	}
}

func TestRaisedRockCastsShadowAndOccludesAmbientLight(t *testing.T) {
	background := RockGrid{{Center: V{0.5, 1.5}, Polygon: []V{{0, generationMinY}, {generationWidth, generationMinY}, {generationWidth, generationMaxY}, {0, generationMaxY}}, Normal: V3{Z: 1}}}
	block := RockGrid{{Center: V{0.5, 0.5}, Polygon: []V{{0.47, 0.47}, {0.53, 0.47}, {0.53, 0.53}, {0.47, 0.53}}, Z: .070, Normal: V3{Z: 1}, Raised: true}}
	d := newRockDepth(background, block)
	away := (V{-rockLight.X, -rockLight.Y}).Norm()
	shadowed := V{0.5, 0.5}.Add(away.Mul(.055))
	lit := V{0.5, 0.5}.Sub(away.Mul(.055))
	if d.visibility(shadowed, 0) > .1 || d.visibility(lit, 0) < .99 {
		t.Fatal("raised rock does not cast a directional shadow on lower terrain")
	}
	if d.ambient(V{0.535, 0.5}, 0) >= d.ambient(V{0.75, 0.5}, 0) {
		t.Fatal("contact with raised rock does not darken ambient light")
	}
	if d.visibility(V{0.5, 0.5}, .070) < .99 {
		t.Fatal("flat exposed top shadows itself")
	}
	block[0].Z = 0
	flat := newRockDepth(background, block)
	if flat.visibility(shadowed, 0) < .99 {
		t.Fatal("zero-height rock still casts the raised rock's shadow")
	}
}

func TestExposedRockEdgesCancelPartialNeighbors(t *testing.T) {
	grid := RockGrid{
		{Center: V{0.11, 0.11}, Polygon: []V{{0.1, 0.1}, {0.12, 0.1}, {0.12, 0.12}, {0.1, 0.12}}},
		{Center: V{0.125, 0.105}, Polygon: []V{{0.12, 0.1}, {0.13, 0.1}, {0.13, 0.11}, {0.12, 0.11}}},
	}
	edges := exposedRockEdges(grid)
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
		grid[i].Raised, grid[i].Z, grid[i].Normal = true, .060, V3{Z: 1}
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

func TestLightingSeparatesShapeFromMaterialAndVisibility(t *testing.T) {
	up := rockSurfaceColor(rockLight, 1, 1)
	front := rockSurfaceColor(V3{Z: 1}, 1, 1)
	down := rockSurfaceColor((V3{0, 1, .3}).Norm(), 1, 1)
	shadow := rockSurfaceColor(rockLight, 0, .5)
	if up.R <= front.R || front.R <= down.R || shadow.R >= front.R || down.R <= 8 || down.R > 40 || up.R-shadow.R > 160 {
		t.Fatalf("lighting does not describe the relief: up=%v front=%v down=%v shadow=%v", up, front, down, shadow)
	}
	for _, c := range []color.NRGBA{up, front, down, shadow} {
		if c.A != 255 {
			t.Fatal("lighting makes solid rock transparent")
		}
	}
}
