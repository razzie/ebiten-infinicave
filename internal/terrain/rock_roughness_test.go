package terrain

import (
	"math"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestRockRoughnessIsSmallDeterministicAndWorldAnchored(t *testing.T) {
	base := RockGrid{terrainRect(.2, .2, .2, .2), terrainRect(.4, .2, .2, .2)}
	makeGrid := func(top float64, opts ShapeOptions) RockGrid {
		grid := RockGrid{terrainRect(.2, .2-top, .2, .2), terrainRect(.4, .2-top, .2, .2)}
		roughenRockEdges(grid, nil, 42, top, opts)
		return grid
	}
	opts := ShapeOptions{EdgeRoughness: .75, SpikeDensity: .65}
	a, b := makeGrid(0, opts), makeGrid(0, opts)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("edge detail changes on regeneration")
	}
	if !reflect.DeepEqual(base, makeGrid(0, ShapeOptions{})) {
		t.Fatal("zero edge settings changed geometry")
	}
	shifted := makeGrid(-1, opts)
	total := 0
	for i, c := range a {
		if len(c.Polygon) != len(shifted[i].Polygon) {
			t.Fatal("detail depends on section window")
		}
		for j, p := range c.Polygon {
			if p.Sub(shifted[i].Polygon[j].Add(geom.V{Y: -1})).Len() > 1e-9 {
				t.Fatal("detail moved between windows")
			}
			d := math.Inf(1)
			for k, q := range base[i].Polygon {
				d = math.Min(d, geom.SegmentDistanceSquared(p, p, q, base[i].Polygon[(k+1)%len(base[i].Polygon)]))
			}
			if math.Sqrt(d) > .006001 {
				t.Fatalf("oversized tooth: %g", math.Sqrt(d))
			}
			if p.X == .4 && p.Y > .200001 && p.Y < .399999 {
				t.Fatal("shared face edge acquired detail")
			}
		}
		if !simpleRockOutline(c.Polygon) || len(geom.Triangulate(c.Polygon)) != len(c.Polygon)-2 {
			t.Fatal("invalid chipped polygon")
		}
		total += len(c.Polygon)
	}
	if total < 40 {
		t.Fatalf("expected many shallow teeth, got %d vertices", total)
	}
}

func TestRockSpikeDensityKeepsSurvivingTeeth(t *testing.T) {
	makeGrid := func(density float64) RockGrid {
		grid := RockGrid{terrainRect(.2, .2, .3, .3)}
		roughenRockEdges(grid, nil, 42, 0, ShapeOptions{SpikeDensity: density})
		return grid
	}
	zero, low, high := makeGrid(0), makeGrid(.25), makeGrid(1)
	if len(zero[0].Polygon) != 4 {
		t.Fatal("disabled teeth changed edge")
	}
	if len(low[0].Polygon) != len(high[0].Polygon) {
		t.Fatal("density changed candidate placement")
	}
	for i, p := range low[0].Polygon {
		// A retained tooth has exactly the same geometry at higher density.
		onBorder := math.Min(math.Min(math.Abs(p.X-.2), math.Abs(p.X-.5)), math.Min(math.Abs(p.Y-.2), math.Abs(p.Y-.5))) < 1e-10
		if !onBorder && p != high[0].Polygon[i] {
			t.Fatal("surviving tooth changed shape")
		}
	}
}

func TestRockRoughnessReplacesTallNeedlesWithSmallTeeth(t *testing.T) {
	poly := []geom.V{{X: .2, Y: .2}, {X: .4, Y: .2}, {X: .4, Y: .294}, {X: .425, Y: .3}, {X: .4, Y: .306}, {X: .4, Y: .4}, {X: .2, Y: .4}}
	grid := RockGrid{terrainRock(poly)}
	roughenRockEdges(grid, nil, 42, 0, DefaultShapeOptions())
	for _, p := range grid[0].Polygon {
		if p.X > .410 {
			t.Fatalf("tall needle survived: %v", p)
		}
	}
	if len(grid[0].Polygon) <= len(poly) {
		t.Fatal("smaller teeth were not added")
	}
}
