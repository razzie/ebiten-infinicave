package main

import (
	"image/color"
	"math"
	"math/rand"
	"reflect"
	"testing"
)

func TestForegroundVinesStayOnVisibleRock(t *testing.T) {
	// Bright and shadowed rock share a continuous surface, with a real gap
	// to the right. The growth field must use geometry instead of shading.
	rock := RockGrid{
		{Polygon: []V{{0, 0}, {400, 0}, {400, H}, {0, H}}, Color: color.NRGBA{220, 210, 190, 255}},
		{Polygon: []V{{400, 0}, {700, 0}, {700, H}, {400, H}}, Color: color.NRGBA{8, 8, 8, 255}},
		{Polygon: []V{{800, 0}, {W, 0}, {W, H}, {800, H}}},
	}
	field := newForegroundVineTerrain(rock, nil)
	for _, p := range []V{{200, 1500}, {500, 1500}, {400, 1500}} {
		if field.growthSpace(p) < 15 {
			t.Fatalf("surface shading blocked growth at %v", p)
		}
	}
	for _, p := range []V{{750, 1500}, {900, 1500}, {5, 1500}, {700, 1500}} {
		if field.growthSpace(p) > 0 {
			t.Fatalf("surface vines can grow off visible rock at %v", p)
		}
	}
	if rock[0].Color.R != 220 || rock[0].Polygon[0].X != 0 {
		t.Fatal("building growth support modified the rendered rock")
	}
	generate := func(seed int64) []Vine {
		return generateForegroundVines(rock, nil, rand.New(rand.NewSource(seed)))
	}
	vines := generate(42)
	if len(vines) < 2 || !reflect.DeepEqual(vines, generate(42)) || reflect.DeepEqual(vines, generate(43)) {
		t.Fatal("foreground vines need branching, reproducible, seed-dependent growth")
	}
	trunks := 0
	for i, v := range vines {
		if v.Parent < 0 {
			trunks++
		}
		if !v.Foreground || (v.Points[0].Radius != 0 && v.Parent < 0) || v.Points[len(v.Points)-1].Radius != 0 {
			t.Fatal("foreground vine lost its material or tapered tips")
		}
		for _, p := range v.Points {
			if field.growthSpace(p.P)+1e-6 < p.Radius {
				t.Fatalf("vine ribbon exceeds its supporting rock: %+v", p)
			}
		}
		if v.Parent >= 0 {
			if v.Parent >= i || v.Points[0].P != vines[v.Parent].Points[v.Joint].P {
				t.Fatal("foreground branch detached from its parent")
			}
		}
	}
	if trunks > 2 {
		t.Fatalf("foreground vines should remain sparse, got %d trunks", trunks)
	}
	if got := generateForegroundVines(nil, nil, rand.New(rand.NewSource(42))); len(got) != 0 {
		t.Fatal("foreground vines grew without supporting rock")
	}
}

func TestForegroundVinesKeepClearOfGuideLines(t *testing.T) {
	rock := testRockGrid([]V{{500, 1500}}, []color.NRGBA{{100, 90, 80, 255}})
	guides := []Guide{splineGuide([]V{{350, 0}, {420, 1000}, {300, 2000}, {380, H}}, 1)}
	field := newForegroundVineTerrain(rock, guides)
	guide := &guides[0]
	for _, s := range []float64{0, 750, 1500, 2250, guide.S[len(guide.S)-1]} {
		p, _, normal := guide.frameAt(s)
		for _, side := range []float64{-1, 1} {
			if field.growthSpace(p.Add(normal.Mul(side*(foregroundVineGuideClearance-2)))) != 0 {
				t.Fatal("foreground vines can approach a guide line on one side")
			}
		}
	}
	vines := generateForegroundVines(rock, guides, rand.New(rand.NewSource(42)))
	if len(vines) == 0 {
		t.Fatal("guide avoidance blocked the entire supporting rock")
	}
	for _, v := range vines {
		for j, p := range v.Points {
			if guide.project(p.P).Dist-p.Radius < foregroundVineGuideClearance {
				t.Fatalf("foreground vine ribbon approaches a guide: %+v", p)
			}
			if j > 0 {
				prev := v.Points[j-1]
				mid := lerpV(prev.P, p.P, .5)
				if guide.project(mid).Dist-math.Max(prev.Radius, p.Radius) < foregroundVineGuideClearance {
					t.Fatal("vine segment crosses the guide exclusion band")
				}
			}
		}
	}
}

func TestForegroundVinesUseMutedInheritedColor(t *testing.T) {
	vines := []Vine{
		{Parent: -1, Foreground: true, Points: []VinePoint{{V{100, 80}, 3}, {V{100, 100}, 3}, {V{100, 120}, 0}}},
		{Parent: 0, Joint: 1, Depth: 1, Foreground: true, Points: []VinePoint{{V{100, 100}, 2}, {V{120, 120}, 0}}},
	}
	palette := vinePalette(vines, 0)
	if max(palette[3].R, palette[3].G, palette[3].B) > 120 || int(palette[3].R)-int(palette[3].B) > 50 || vinePalette(vines, 1) != palette {
		t.Fatal("foreground stems need subdued highlights inherited by their branches")
	}
	if got := vineJoinColor(vines, 1, V{100, 100}, palette[0]); got != vineBandColorFrom(palette, 0) {
		t.Fatal("foreground fork has a material discontinuity")
	}
	meshes := prepareForegroundVines(vines)
	if len(meshes) != 2*len(vines) {
		t.Fatal("foreground vines lost their contact shadows")
	}
	for _, mesh := range meshes {
		checkMesh(t, mesh)
	}
	if meshes[0].vertices[0].ColorA >= 1 || meshes[len(vines)].vertices[0].ColorA != 1 {
		t.Fatal("contact shadows must precede the opaque stems")
	}
}
