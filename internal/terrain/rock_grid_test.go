package terrain

import (
	"math"
	"math/rand"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestBackgroundKeepsWholeVoronoiCellsAndExposedEdges(t *testing.T) {
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	seeds := worldSeedsInRange(42, 0, noise, BackgroundMinX, BackgroundMaxX)
	footprint := newRockFootprint(RockGrid{terrainRect(.3, .3, .4, .1)})
	grid := newBackgroundRockGrid(42, seeds, footprint)
	cells := voronoiCellsInRange(seeds, BackgroundMinX, BackgroundMaxX)
	bySite := make(map[geom.V][]geom.V, len(seeds))
	for i, p := range seeds {
		bySite[p] = cells[i]
	}
	if len(grid) == 0 || len(grid) == len(seeds) {
		t.Fatal("background must retain formations and borders with open space between them")
	}
	for _, cell := range grid {
		if !reflect.DeepEqual(cell.Polygon, bySite[cell.Center]) {
			t.Fatal("background changed a Voronoi cell's edges or inserted contour vertices")
		}
	}
	// Whole boundary cells protrude beyond the smooth selection field.
	// Clipping them to that field would recreate the smooth silhouette.
	jagged := false
	for _, edge := range ExposedRockEdges(grid) {
		if edge.A.X < .2 || edge.A.X > .8 {
			continue
		}
		a := footprint.backgroundHeight(edge.A) - rockContourHeight
		b := footprint.backgroundHeight(edge.B) - rockContourHeight
		if a < -.005 && b < -.005 {
			jagged = true
			break
		}
	}
	if !jagged {
		t.Fatal("background silhouette does not follow exposed Voronoi edges")
	}
}

func TestBackgroundPaddingHolesAreSeededGaps(t *testing.T) {
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	seeds := worldSeedsInRange(42, 0, noise, BackgroundMinX, BackgroundMaxX)
	footprint := newRockFootprint(nil)
	a := newBackgroundRockGrid(42, seeds, footprint)
	if !reflect.DeepEqual(a, newBackgroundRockGrid(42, seeds, footprint)) {
		t.Fatal("padding holes changed on regeneration")
	}
	if reflect.DeepEqual(a, newBackgroundRockGrid(1, seeds, footprint)) {
		t.Fatal("world seed did not change padding holes")
	}
	retained := make(map[geom.V]bool, len(a))
	for _, cell := range a {
		retained[cell.Center] = true
	}
	missing := [2]int{}
	for _, p := range seeds {
		if p.Y < .1 || p.Y > .9 || retained[p] {
			continue
		}
		// Far inside the border strips every cell was solid before pockets
		// were introduced. These gaps must be real geometry omissions.
		if p.X > -.4 && p.X < -.1 {
			missing[0]++
		}
		if p.X > 1.1 && p.X < 1.4 {
			missing[1]++
		}
	}
	if missing[0] < 5 || missing[1] < 5 {
		t.Fatalf("both padding strips need visible holes, missing cells: %v", missing)
	}
}

func TestForegroundGridStaysInsideHorizontalScreenInset(t *testing.T) {
	grid := RockGrid{
		{Center: geom.V{X: 0.01, Y: 0.05}, Polygon: []geom.V{{X: 0, Y: 0}, {X: 0.04, Y: 0}, {X: 0.04, Y: 0.1}, {X: 0, Y: 0.1}}, Raised: true},
		{Center: geom.V{X: generationWidth - 0.01, Y: 0.05}, Polygon: []geom.V{{X: generationWidth - 0.04, Y: 0}, {X: generationWidth, Y: 0}, {X: generationWidth, Y: 0.1}, {X: generationWidth - 0.04, Y: 0.1}}, Raised: true},
	}
	original := make([][]geom.V, len(grid))
	for i := range grid {
		original[i] = append([]geom.V(nil), grid[i].Polygon...)
	}

	clipped := InsetForegroundGrid(grid)
	if len(clipped) != len(grid) {
		t.Fatalf("inset retained %d cells, want %d", len(clipped), len(grid))
	}
	for _, cell := range clipped {
		for _, p := range cell.Polygon {
			if p.X < foregroundScreenInset-1e-9 || p.X > generationWidth-foregroundScreenInset+1e-9 {
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

func TestBackgroundSeedsWorkWithoutGuides(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	noise := NewPerlin(rng)
	seeds := generateSeeds(rng, 80, noise)
	relaxSeeds(seeds, noise)
	for _, p := range seeds {
		spacing := desiredSpacing(p, noise)
		if math.IsNaN(p.X) || math.IsNaN(p.Y) || p.X < 0 || p.X > generationWidth || p.Y < GenerationMinY || p.Y > generationMaxY {
			t.Fatalf("invalid background seed: %v", p)
		}
		if math.IsNaN(spacing) || spacing < .022 || spacing > .030 {
			t.Fatalf("invalid background spacing at %v: %v", p, spacing)
		}
	}
}

func TestWorldSeedsMatchOverlappingWindows(t *testing.T) {
	seed := int64(42)
	aTop, bTop := SectionTop(0), SectionTop(1)
	noise := NewPerlin(rand.New(rand.NewSource(seed)))
	noise.OffsetY = aTop
	a := worldSeeds(seed, aTop, noise)
	noise.OffsetY = bTop
	b := worldSeeds(seed, bTop, noise)
	collect := func(seeds []geom.V, top float64) []geom.V {
		var result []geom.V
		for _, p := range seeds {
			p.Y += top
			if p.Y > -1.900 && p.Y < -.100 {
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
