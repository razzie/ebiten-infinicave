package terrain

import (
	"math"
	"math/rand"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestExtendedBackgroundVoronoiCoverage(t *testing.T) {
	seeds := []geom.V{{X: -.4, Y: -.5}, {X: -.2, Y: 1.4}, {X: .3, Y: .4}, {X: .7, Y: -.4}, {X: 1.2, Y: .3}, {X: 1.4, Y: 1.6}}
	cells := voronoiCellsInRange(seeds, BackgroundMinX, BackgroundMaxX)
	area := 0.0
	for i, cell := range cells {
		// Compare the bounded search with clipping against every site.
		want := []geom.V{{X: -.5, Y: -1}, {X: 1.5, Y: -1}, {X: 1.5, Y: 2}, {X: -.5, Y: 2}}
		for j, other := range seeds {
			if i != j {
				want = geom.ClipHalfPlane(want, other.Sub(seeds[i]), .5*(other.Len2()-seeds[i].Len2()))
			}
		}
		want = geom.OrderPolygon(want)
		if len(cell) != len(want) {
			t.Fatalf("cell %d has %d vertices, want %d", i, len(cell), len(want))
		}
		for j, p := range cell {
			if p.Sub(want[j]).Len() > 1e-9 {
				t.Fatalf("cell %d vertex %d: %v, want %v", i, j, p, want[j])
			}
		}
		area += geom.PolygonArea(cell)
	}
	if math.Abs(area-6) > 1e-9 {
		t.Fatalf("extended background area %v, want 6", area)
	}
}

func TestVoronoiCellsMatchReference(t *testing.T) {
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	seeds := worldSeeds(42, -1, noise)
	cells := voronoiCells(seeds)
	for i := range seeds {
		want := voronoiCell(i, seeds)
		if len(cells[i]) != len(want) {
			t.Fatalf("cell %d: %d vertices, want %d", i, len(cells[i]), len(want))
		}
		for k := range want {
			if cells[i][k].Sub(want[k]).Len() > 1e-9 {
				t.Fatalf("cell %d vertex %d: %v, want %v", i, k, cells[i][k], want[k])
			}
		}
	}
}
