package main

import (
	"image/color"
	"math"
	"math/rand"
	"testing"
)

func TestReliefProfileHasBevelCrestAndBroadFlank(t *testing.T) {
	const width, height, bevel = 180.0, 72.0, 16.0
	if ridgeProfile(-1, width, height, bevel) != 0 || ridgeProfile(width, width, height, bevel) != 0 {
		t.Fatal("relief extends outside its footprint")
	}
	if ridgeProfile(0, width, height, bevel) <= 0 || ridgeProfile(bevel, width, height, bevel) != height {
		t.Fatal("exposed lip has no thickness or crest")
	}
	for d := 1.0; d < width; d++ {
		change := ridgeProfile(d, width, height, bevel) - ridgeProfile(d-1, width, height, bevel)
		if (d <= bevel && change <= 0) || (d > bevel && change >= 0) {
			t.Fatalf("relief changes slope at the wrong place: d=%v", d)
		}
	}
}

func TestReliefHeightAndSiteDeformationUseWorldCoordinates(t *testing.T) {
	a := []Guide{splineGuide([]V{{100, 400}, {500, 350}, {900, 400}}, 1)}
	b := []Guide{splineGuide([]V{{100, 400}, {500, 350}, {900, 400}}, 1)}
	b[0].translateY(1000)
	n1, n2 := NewPerlin(rand.New(rand.NewSource(42))), NewPerlin(rand.New(rand.NewSource(42)))
	n2.OffsetY = -1000
	for y := 350.0; y < 700; y += 13 {
		p, q := V{450, y}, V{450, y + 1000}
		if math.Abs(reliefHeight(p, a, n1, nil)-reliefHeight(q, b, n2, nil)) > 1e-9 {
			t.Fatal("relief changes between generation windows")
		}
		x, z := reliefSeeds([]V{p}, a)[0], reliefSeeds([]V{q}, b)[0]
		z.Y -= 1000
		if x.Sub(z).Len() > 1e-9 {
			t.Fatal("facet deformation changes between generation windows")
		}
	}
}

func TestRaisedRockCastsShadowAndOccludesAmbientLight(t *testing.T) {
	background := RockGrid{{Center: V{500, 1500}, Polygon: []V{{0, 0}, {W, 0}, {W, H}, {0, H}}, Normal: V3{Z: 1}}}
	block := RockGrid{{Center: V{500, 500}, Polygon: []V{{470, 470}, {530, 470}, {530, 530}, {470, 530}}, Z: 70, Normal: V3{Z: 1}, Raised: true}}
	d := newRockDepth(background, block)
	away := (V{-rockLight.X, -rockLight.Y}).Norm()
	shadowed := V{500, 500}.Add(away.Mul(55))
	lit := V{500, 500}.Sub(away.Mul(55))
	if d.visibility(shadowed, 0) > .1 || d.visibility(lit, 0) < .99 {
		t.Fatal("raised rock does not cast a directional shadow on lower terrain")
	}
	if d.ambient(V{535, 500}, 0) >= d.ambient(V{750, 500}, 0) {
		t.Fatal("contact with raised rock does not darken ambient light")
	}
	if d.visibility(V{500, 500}, 70) < .99 {
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
		{Center: V{110, 110}, Polygon: []V{{100, 100}, {120, 100}, {120, 120}, {100, 120}}},
		{Center: V{125, 105}, Polygon: []V{{120, 100}, {130, 100}, {130, 110}, {120, 110}}},
	}
	edges := exposedRockEdges(grid)
	length := 0.0
	for _, e := range edges {
		length += e.B.Sub(e.A).Len()
		if e.A.X == 120 && e.B.X == 120 && (e.A.Y+e.B.Y)*.5 < 110 {
			t.Fatal("shared partial edge incorrectly exposes a side wall")
		}
	}
	if math.Abs(length-100) > 1e-9 {
		t.Fatalf("wrong exposed perimeter: %v", length)
	}
	for i := range grid {
		grid[i].Raised, grid[i].Z, grid[i].Normal = true, 60, V3{Z: 1}
		grid[i].Shadow, grid[i].Ambient = 1, 1
		grid[i].Color = color.NRGBA{R: 100, G: 100, B: 100, A: 255}
	}
	vertices, indices := appendRockWalls(nil, nil, grid, edges, "clay")
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
