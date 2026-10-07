package terrain

import (
	"image/color"
	"math"
	"math/rand"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestForegroundVinesStayOnVisibleRock(t *testing.T) {
	// Bright and shadowed rock share a continuous surface, with a real gap
	// to the right. The growth field must use geometry instead of shading.
	rock := RockGrid{
		{Polygon: []geom.V{{X: 0, Y: GenerationMinY}, {X: 0.4, Y: GenerationMinY}, {X: 0.4, Y: generationMaxY}, {X: 0, Y: generationMaxY}}, Color: color.NRGBA{220, 210, 190, 255}},
		{Polygon: []geom.V{{X: 0.4, Y: GenerationMinY}, {X: 0.7, Y: GenerationMinY}, {X: 0.7, Y: generationMaxY}, {X: 0.4, Y: generationMaxY}}, Color: color.NRGBA{8, 8, 8, 255}},
		{Polygon: []geom.V{{X: 0.8, Y: GenerationMinY}, {X: generationWidth, Y: GenerationMinY}, {X: generationWidth, Y: generationMaxY}, {X: 0.8, Y: generationMaxY}}},
	}
	field := newForegroundVineTerrain(rock, nil)
	for _, p := range []geom.V{{X: 0.2, Y: 1.5}, {X: 0.5, Y: 1.5}, {X: 0.4, Y: 1.5}} {
		if field.growthSpace(p) < .015 {
			t.Fatalf("surface shading blocked growth at %v", p)
		}
	}
	for _, p := range []geom.V{{X: 0.75, Y: 1.5}, {X: 0.9, Y: 1.5}, {X: 0.005, Y: 1.5}, {X: 0.7, Y: 1.5}} {
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
	rock := testRockGrid([]geom.V{{X: 0.5, Y: 1.5}}, []color.NRGBA{{100, 90, 80, 255}})
	guides := []Guide{SplineGuide([]geom.V{{X: 0.35, Y: GenerationMinY}, {X: 0.42, Y: 0}, {X: 0.3, Y: 1}, {X: 0.38, Y: generationMaxY}}, 1)}
	field := newForegroundVineTerrain(rock, guides)
	guide := &guides[0]
	for _, s := range []float64{0, .750, 1.500, 2.250, guide.S[len(guide.S)-1]} {
		p, _, normal := guide.frameAt(s)
		for _, side := range []float64{-1, 1} {
			if field.growthSpace(p.Add(normal.Mul(side*(foregroundVineGuideClearance-.002)))) != 0 {
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
				mid := geom.LerpVector(prev.P, p.P, .5)
				if guide.project(mid).Dist-math.Max(prev.Radius, p.Radius) < foregroundVineGuideClearance {
					t.Fatal("vine segment crosses the guide exclusion band")
				}
			}
		}
	}
}
