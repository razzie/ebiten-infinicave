package infinicave

import (
	"math"
	"math/rand"
	"testing"
)

func TestRockNormalsFollowNeighborHeights(t *testing.T) {
	// Unequal, non-axis-aligned spacing must still recover a planar slope.
	grid := RockGrid{{Center: V{50, 50}}, {Center: V{23, 42}}, {Center: V{61, 17}}, {Center: V{85, 61}}, {Center: V{39, 79}}}
	for _, slope := range []V{{}, {0, 1}, {0, -1}, {.7, -.3}} {
		for i := range grid {
			grid[i].Z = 15 + grid[i].Center.Dot(slope)
		}
		got := rockNormal(grid, 0, []int{1, 2, 3, 4})
		want := (V3{-slope.X, -slope.Y, 1}).Norm()
		if math.Abs(got.X-want.X)+math.Abs(got.Y-want.Y)+math.Abs(got.Z-want.Z) > 1e-12 {
			t.Fatalf("slope %v: got %v, want %v", slope, got, want)
		}
	}
	flat := surfaceLight(V3{Z: 1})
	up, down := surfaceLight((V3{0, -1, 1}).Norm()), surfaceLight((V3{0, 1, 1}).Norm())
	if up <= flat || flat <= down {
		t.Fatalf("lighting does not favor upward faces: up=%v flat=%v down=%v", up, flat, down)
	}
	if got := rockNormal(grid, 0, nil); got != (V3{Z: 1}) {
		t.Fatalf("isolated cell has invalid normal: %v", got)
	}
	grid[1].Center, grid[1].Z = V{50, 40}, grid[0].Z-10
	if got := rockNormal(grid, 0, []int{1}); math.Abs(got.Y+math.Sqrt(.5)) > 1e-12 {
		t.Fatalf("single neighbor lost its slope: %v", got)
	}
}

func TestRockNeighborsIncludePartialEdgesButNotCorners(t *testing.T) {
	grid := RockGrid{
		{Polygon: []V{{0, 0}, {20, 0}, {20, 20}, {0, 20}}},
		{Polygon: []V{{20, 0}, {30, 0}, {30, 10}, {20, 10}}},
		{Polygon: []V{{20, 20}, {30, 20}, {30, 30}, {20, 30}}},
	}
	neighbors := rockNeighbors(grid)
	if len(neighbors[0]) != 1 || neighbors[0][0] != 1 || len(neighbors[1]) != 1 || len(neighbors[2]) != 0 {
		t.Fatalf("incorrect boundary adjacency: %v", neighbors)
	}
}

func TestGuideFacingOverridesHeightsOnBothSides(t *testing.T) {
	guides := []Guide{splineGuide([]V{{-10, 500}, {W + 10, 500}}, 1)}
	grid := guideRockFaces([]V{{250, 480}, {750, 480}, {250, 800}, {750, 800}}, guides)
	shapeRockGrid(grid, guides, NewPerlin(rand.New(rand.NewSource(42))))
	touching, distant := 0, 0
	for i, cell := range grid {
		contact := false
		for _, p := range cell.Polygon {
			contact = contact || math.Abs(p.Y-500) < 1e-7
		}
		if contact {
			touching++
			if math.Abs(cell.Normal.X) > 1e-9 || cell.Normal.Y*(500-cell.Center.Y) <= 0 {
				t.Fatalf("guide neighbor faces away: %v normal=%v", cell.Center, cell.Normal)
			}
		} else {
			distant++
			want := rockNormal(grid, i, rockNeighbors(grid)[i])
			if cell.Normal != want {
				t.Fatalf("guide overrode a non-neighbor: %v", cell.Center)
			}
		}
	}
	if touching != 4 || distant != 2 {
		t.Fatalf("unexpected guide contact counts: %d/%d", touching, distant)
	}
}

func TestRockLightingPreservesOccupancyAndDarkFlanks(t *testing.T) {
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	guides := []Guide{splineGuide([]V{{100, 400}, {900, 400}}, 1)}
	for y := 0.0; y < H; y += 20 {
		for x := 0.0; x < W; x += 20 {
			p := V{x, y}
			darkForeground := guideSurfaceColor(p, guides, noise, nil, V3{Y: 1})
			if darkForeground.A != 0 && (darkForeground.R <= 8 || darkForeground.R > 40) {
				t.Fatalf("foreground flank brightness is outside the raised palette: %v", darkForeground)
			}
			lit := guideSurfaceColor(p, guides, noise, nil, V3{0, -.8, .6})
			if lit.A != darkForeground.A {
				t.Fatal("surface lighting changed guide transparency")
			}
			if lit.A != 0 && lit.R <= darkForeground.R {
				t.Fatalf("guide-facing foreground is not brighter: lit=%v flank=%v", lit, darkForeground)
			}
		}
	}
}
