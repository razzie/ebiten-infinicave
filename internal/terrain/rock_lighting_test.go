package terrain

import (
	"image/color"
	"math"
	"math/rand"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestSpatialRockNeighborsMatchAllPairs(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	var sites []geom.V
	for i := 0; i < 160; i++ {
		sites = append(sites, geom.V{X: rng.Float64(), Y: GenerationMinY + rng.Float64()*GenerationHeight})
	}
	grid := newRockGrid(sites, func(geom.V) color.NRGBA { return color.NRGBA{A: 255} })
	want := make([][]int, len(grid))
	for i := range grid {
		for j := i + 1; j < len(grid); j++ {
			if mergeableBorder(grid[i].Polygon, grid[j].Polygon, nil) > mergeTolerance {
				want[i] = append(want[i], j)
				want[j] = append(want[j], i)
			}
		}
	}
	if got := RockNeighbors(grid); !reflect.DeepEqual(got, want) {
		t.Fatal("spatial search changed rock adjacency or neighbor order")
	}
}

func TestRockNormalsFollowNeighborHeights(t *testing.T) {
	// Unequal, non-axis-aligned spacing must still recover a planar slope.
	grid := RockGrid{{Center: geom.V{X: 0.05, Y: 0.05}}, {Center: geom.V{X: 0.023, Y: 0.042}}, {Center: geom.V{X: 0.061, Y: 0.017}}, {Center: geom.V{X: 0.085, Y: 0.061}}, {Center: geom.V{X: 0.039, Y: 0.079}}}
	for _, slope := range []geom.V{{}, {X: 0, Y: 1}, {X: 0, Y: -1}, {X: .7, Y: -.3}} {
		for i := range grid {
			grid[i].Z = .015 + grid[i].Center.Dot(slope)
		}
		got := rockNormal(grid, 0, []int{1, 2, 3, 4})
		want := (geom.V3{X: -slope.X, Y: -slope.Y, Z: 1}).Norm()
		if math.Abs(got.X-want.X)+math.Abs(got.Y-want.Y)+math.Abs(got.Z-want.Z) > 1e-12 {
			t.Fatalf("slope %v: got %v, want %v", slope, got, want)
		}
	}
	flat := SurfaceLight(geom.V3{Z: 1})
	up, down := SurfaceLight((geom.V3{X: 0, Y: -1, Z: 1}).Norm()), SurfaceLight((geom.V3{X: 0, Y: 1, Z: 1}).Norm())
	if up <= flat || flat <= down {
		t.Fatalf("lighting does not favor upward faces: up=%v flat=%v down=%v", up, flat, down)
	}
	if got := rockNormal(grid, 0, nil); got != (geom.V3{Z: 1}) {
		t.Fatalf("isolated cell has invalid normal: %v", got)
	}
	grid[1].Center, grid[1].Z = geom.V{X: 0.05, Y: 0.04}, grid[0].Z-.010
	if got := rockNormal(grid, 0, []int{1}); math.Abs(got.Y+math.Sqrt(.5)) > 1e-12 {
		t.Fatalf("single neighbor lost its slope: %v", got)
	}
}

func TestRockNeighborsIncludePartialEdgesButNotCorners(t *testing.T) {
	grid := RockGrid{
		{Polygon: []geom.V{{X: 0, Y: 0}, {X: 0.02, Y: 0}, {X: 0.02, Y: 0.02}, {X: 0, Y: 0.02}}},
		{Polygon: []geom.V{{X: 0.02, Y: 0}, {X: 0.03, Y: 0}, {X: 0.03, Y: 0.01}, {X: 0.02, Y: 0.01}}},
		{Polygon: []geom.V{{X: 0.02, Y: 0.02}, {X: 0.03, Y: 0.02}, {X: 0.03, Y: 0.03}, {X: 0.02, Y: 0.03}}},
	}
	neighbors := RockNeighbors(grid)
	if len(neighbors[0]) != 1 || neighbors[0][0] != 1 || len(neighbors[1]) != 1 || len(neighbors[2]) != 0 {
		t.Fatalf("incorrect boundary adjacency: %v", neighbors)
	}
}

func TestGuideFacingOverridesHeightsOnBothSides(t *testing.T) {
	guides := []Guide{SplineGuide([]geom.V{{X: -0.01, Y: 0.5}, {X: generationWidth + 0.01, Y: 0.5}}, 1)}
	grid := guideRockFaces([]geom.V{{X: 0.25, Y: 0.48}, {X: 0.75, Y: 0.48}, {X: 0.25, Y: 0.8}, {X: 0.75, Y: 0.8}}, guides)
	shapeRockGrid(grid, guides, NewPerlin(rand.New(rand.NewSource(42))))
	touching, distant := 0, 0
	for i, cell := range grid {
		contact := false
		for _, p := range cell.Polygon {
			contact = contact || math.Abs(p.Y-.500) < 1e-10
		}
		if contact {
			touching++
			if math.Abs(cell.Normal.X) > 1e-9 || cell.Normal.Y*(.500-cell.Center.Y) <= 0 {
				t.Fatalf("guide neighbor faces away: %v normal=%v", cell.Center, cell.Normal)
			}
		} else {
			distant++
			want := rockNormal(grid, i, RockNeighbors(grid)[i])
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
	guides := []Guide{SplineGuide([]geom.V{{X: 0.1, Y: 0.4}, {X: 0.9, Y: 0.4}}, 1)}
	for y := float64(GenerationMinY); y < generationMaxY; y += 0.02 {
		for x := 0.0; x < generationWidth; x += 0.02 {
			p := geom.V{X: x, Y: y}
			darkForeground := guideSurfaceColor(p, guides, noise, nil, geom.V3{Y: 1})
			if darkForeground.A != 0 && (darkForeground.R <= 8 || darkForeground.R > 40) {
				t.Fatalf("foreground flank brightness is outside the raised palette: %v", darkForeground)
			}
			lit := guideSurfaceColor(p, guides, noise, nil, geom.V3{X: 0, Y: -.8, Z: .6})
			if lit.A != darkForeground.A {
				t.Fatal("surface lighting changed guide transparency")
			}
			if lit.A != 0 && lit.R <= darkForeground.R {
				t.Fatalf("guide-facing foreground is not brighter: lit=%v flank=%v", lit, darkForeground)
			}
		}
	}
}
