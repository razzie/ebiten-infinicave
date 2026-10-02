package main

import (
	"math"
	"math/rand"
	"reflect"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
)

func TestWorldSeedsMatchOverlappingWindows(t *testing.T) {
	seed := int64(42)
	aTop, bTop := sectionWindowTop(0), sectionWindowTop(1)
	noise := NewPerlin(rand.New(rand.NewSource(seed)))
	noise.OffsetY = aTop
	a := worldSeeds(seed, aTop, noise)
	noise.OffsetY = bTop
	b := worldSeeds(seed, bTop, noise)
	collect := func(seeds []V, top float64) []V {
		var result []V
		for _, p := range seeds {
			p.Y += top
			if p.Y > -1900 && p.Y < -100 {
				result = append(result, p)
			}
		}
		return result
	}
	left, right := collect(a, aTop), collect(b, bTop)
	if len(left) != len(right) {
		t.Fatal("overlapping sections use different terrain site counts")
	}
	for i, p := range left {
		if p.Sub(right[i]).Len() > 1e-9 {
			t.Fatal("overlapping sections use different terrain sites")
		}
	}
}

func TestForegroundGridStaysInsideHorizontalScreenInset(t *testing.T) {
	grid := RockGrid{
		{Center: V{10, 50}, Polygon: []V{{0, 0}, {40, 0}, {40, 100}, {0, 100}}, Raised: true},
		{Center: V{W - 10, 50}, Polygon: []V{{W - 40, 0}, {W, 0}, {W, 100}, {W - 40, 100}}, Raised: true},
	}
	original := make([][]V, len(grid))
	for i := range grid {
		original[i] = append([]V(nil), grid[i].Polygon...)
	}

	clipped := insetForegroundGrid(grid)
	if len(clipped) != len(grid) {
		t.Fatalf("inset retained %d cells, want %d", len(clipped), len(grid))
	}
	for _, cell := range clipped {
		for _, p := range cell.Polygon {
			if p.X < foregroundScreenInset-1e-9 || p.X > W-foregroundScreenInset+1e-9 {
				t.Fatalf("foreground vertex reaches horizontal screen edge: %v", p)
			}
		}
	}
	for i := range grid {
		if !reflect.DeepEqual(grid[i].Polygon, original[i]) {
			t.Fatal("render inset mutated the source geometry")
		}
	}
}

func TestWorldSectionSeam(t *testing.T) {
	a, b := buildSection(42, 0), buildSection(42, 1)
	// Compare whole rock faces in a strip around the shared seam, not just
	// sample colors. Geometry, relief, shadowing, and material must agree.
	type face struct {
		color           [4]uint8
		polygon         []V
		seed            int64
		z               float64
		normal          V3
		shadow, ambient float64
	}
	collect := func(grid RockGrid, top float64) map[[2]int64]face {
		result := make(map[[2]int64]face)
		for _, c := range grid {
			y := c.Center.Y + top
			if y < -1120 || y > -880 {
				continue
			}
			poly := make([]V, len(c.Polygon))
			for i, p := range c.Polygon {
				poly[i] = V{p.X, p.Y + top}
			}
			key := [2]int64{int64(math.Round(c.Center.X * 1e5)), int64(math.Round(y * 1e5))}
			result[key] = face{[4]uint8{c.Color.R, c.Color.G, c.Color.B, c.Color.A}, poly, cellSeed(42, c.Center, top), c.Z, c.Normal, c.Shadow, c.Ambient}
		}
		return result
	}
	for i, pair := range [][2]RockGrid{{a.background, b.background}, {a.foreground, b.foreground}} {
		left, right := collect(pair[0], sectionWindowTop(0)), collect(pair[1], sectionWindowTop(1))
		if len(left) == 0 || len(left) != len(right) {
			t.Fatalf("layer %d seam has different faces: %d/%d", i, len(left), len(right))
		}
		for key, x := range left {
			y, ok := right[key]
			if !ok || x.color != y.color || x.seed != y.seed || len(x.polygon) != len(y.polygon) {
				t.Fatalf("layer %d: mismatched face at %v", i, key)
			}
			if math.Abs(x.shadow-y.shadow) > 1e-8 || math.Abs(x.ambient-y.ambient) > 1e-8 || math.Abs(x.z-y.z) > 1e-8 || math.Abs(x.normal.X-y.normal.X)+math.Abs(x.normal.Y-y.normal.Y)+math.Abs(x.normal.Z-y.normal.Z) > 1e-8 {
				t.Fatalf("layer %d: height, normal, or lighting seam at %v", i, key)
			}
			for j, p := range x.polygon {
				if p.Sub(y.polygon[j]).Len() > 1e-6 {
					t.Fatalf("layer %d: polygon seam at %v: %v vs %v", i, key, p, y.polygon[j])
				}
			}
		}
	}
	// At least one full vine in the adjacent sections must span a boundary; its
	// offshoots must remain attached even outside the owner's core section.
	crossing := false
	for _, section := range []sectionData{a, b} {
		for i, v := range section.vines {
			for j, p := range v.Points {
				if j == 0 {
					continue
				}
				prev := v.Points[j-1]
				for _, edge := range []float64{W, 2 * W} {
					if (prev.P.Y-edge)*(p.P.Y-edge) < 0 && prev.Radius > 0 && p.Radius > 0 {
						crossing = true
					}
				}
			}
			if v.Depth > 0 {
				if v.Parent < 0 || v.Parent >= i {
					t.Fatal("cross-section vine lost its parent")
				}
				parent := section.vines[v.Parent]
				if parent.Points[v.Joint].P != v.Points[0].P {
					t.Fatal("cross-section vine branch is detached")
				}
			}
		}
	}
	if !crossing {
		t.Fatal("vines stopped at all section boundaries")
	}
	// Eviction must not change either geometry or parent attachment indices.
	again := buildSection(42, 0)
	if !reflect.DeepEqual(a, again) {
		t.Fatal("revisiting an evicted section changes the scene")
	}
}

func TestStreamingRequestsAndCacheStayBounded(t *testing.T) {
	w := &World{sections: make(map[int64]*worldSection), jobs: make(chan int64, 1)}
	if w.ensure(-800, 800, 0) {
		t.Fatal("unloaded viewport reported ready")
	}
	if id := <-w.jobs; id != 0 {
		t.Fatalf("first request is %d, want floor section", id)
	}
	// Repeated frames must not enqueue duplicate work while the worker is busy.
	w.ensure(-800, 800, 0)
	if len(w.jobs) != 0 {
		t.Fatal("duplicate generation request")
	}
	w.working = false
	for id := int64(0); id < 100; id++ {
		w.sections[id] = &worldSection{terrain: ebiten.NewImage(1, 1), vines: ebiten.NewImage(1, 1)}
	}
	w.prune(-10800, 800, 0)
	if len(w.sections) > 6 {
		t.Fatalf("cache grew with distance: %d sections", len(w.sections))
	}
	if w.sections[0] != nil || w.sections[10] == nil {
		t.Fatal("cache discarded the viewport or retained distant sections")
	}
	if !w.ensure(-10800, 800, 0) {
		t.Fatal("loaded far-up viewport cannot be reached")
	}
	w.prune(-800, 800, 0)
	if w.ensure(-800, 800, 0) {
		t.Fatal("evicted starting area was not requested again")
	}
	for _, s := range w.sections {
		s.terrain.Deallocate()
		s.vines.Deallocate()
	}
}
