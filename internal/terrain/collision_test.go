package terrain

import (
	"math"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestCollisionUnionWithHoleAndPartialSharedBorders(t *testing.T) {
	// Four strips enclose a hole. Their shared edges have different lengths.
	grid := RockGrid{
		terrainRect(0.03, 0.1, 0.08, 0.02), terrainRect(0.03, 0.16, 0.08, 0.02),
		terrainRect(0.03, 0.12, 0.02, 0.04), terrainRect(0.09, 0.12, 0.02, 0.04),
		// A corner-touching rectangle stays a separate block.
		terrainRect(0.11, 0.18, 0.02, 0.02),
	}
	geometry := PrepareTerrainGeometry(SectionData{Foreground: grid}, 0).Collision
	if len(geometry.Polygons) != 3 {
		t.Fatalf("got %d contours, want outer boundary, hole, and separate rectangle", len(geometry.Polygons))
	}
	area, holes := 0.0, 0
	for _, poly := range geometry.Polygons {
		area += geom.PolygonArea(poly)
		if geom.PolygonArea(poly) < 0 {
			holes++
		}
	}
	if math.Abs(area-.0052) > 1e-12 || holes != 1 {
		t.Fatalf("area %v / holes %d; internal borders or hole are incorrect", area, holes)
	}
	for _, p := range []geom.V{{X: 0.04, Y: -0.89}, {X: 0.1, Y: -0.85}, {X: 0.04, Y: -0.84}, {X: 0.03, Y: -0.88}, {X: 0.12, Y: -0.81}} {
		if !geometry.Contains(p) {
			t.Fatalf("solid point %v was missed", p)
		}
	}
	for _, p := range []geom.V{{X: 0.07, Y: -0.85}, {X: 0.02, Y: -0.85}, {X: 0.14, Y: -0.85}} {
		if geometry.Contains(p) {
			t.Fatalf("empty point %v was filled", p)
		}
	}
	if got := PrepareTerrainGeometry(SectionData{Foreground: grid}, 0).Collision; !reflect.DeepEqual(got, geometry) {
		t.Fatal("collision contour ordering is nondeterministic")
	}
	for _, tolerance := range []float64{.001, .010, 1} {
		g := PrepareTerrainGeometry(SectionData{Foreground: grid}, tolerance).Collision
		if g.Contains(geom.V{X: 0.07, Y: -0.85}) || !g.Contains(geom.V{X: 0.04, Y: -0.85}) {
			t.Fatal("simplification lost the hole or its enclosing rock")
		}
	}
}

func TestCollisionSimplificationToleranceAndSeams(t *testing.T) {
	poly := []geom.V{{X: 0.03, Y: 0.98}, {X: 0.04, Y: 0.9801}, {X: 0.05, Y: 0.98}, {X: 0.06, Y: 0.9801}, {X: 0.07, Y: 0.98},
		{X: 0.07, Y: 1.02}, {X: 0.06, Y: 1.0201}, {X: 0.05, Y: 1.02}, {X: 0.04, Y: 1.0201}, {X: 0.03, Y: 1.02}}
	before := append([]geom.V(nil), poly...)
	simplified := simplifyCollisionLoop(poly, .0002)
	if len(simplified) >= len(poly) {
		t.Fatal("tolerance did not reduce boundary complexity")
	}
	if !reflect.DeepEqual(poly, before) {
		t.Fatal("simplification mutated visual geometry")
	}
	for _, p := range poly {
		best := math.Inf(1)
		for i, a := range simplified {
			best = math.Min(best, geom.SegmentDistanceSquared(p, p, a, simplified[(i+1)%len(simplified)]))
		}
		if best > .0002*.0002+1e-15 {
			t.Fatalf("vertex %v exceeds tolerance: %v", p, math.Sqrt(best))
		}
	}
	for _, seam := range []geom.V{{X: 0.03, Y: 1}, {X: 0.07, Y: 1}} {
		found := false
		for _, p := range simplified {
			found = found || p == seam
		}
		if !found {
			t.Fatalf("exact section crossing %v was removed", seam)
		}
	}
	// Deep concavities and narrow features must never become self-crossing.
	concave := []geom.V{{X: 0.03, Y: 1.1}, {X: 0.07, Y: 1.1}, {X: 0.07, Y: 1.11}, {X: 0.04, Y: 1.11}, {X: 0.04, Y: 1.14}, {X: 0.03, Y: 1.14}}
	for _, tolerance := range []float64{.001, .010, 1} {
		g := simplifyCollisionLoop(concave, tolerance)
		if len(g) < 3 || !simpleCollisionLoop(g) || geom.PolygonArea(g) <= 0 {
			t.Fatal("simplification produced an invalid polygon")
		}
	}
}

func TestGeneratedCollisionMatchesRockFaces(t *testing.T) {
	data := NewSectionBuilder(42, testCurlSection).Build(0)
	h := PrepareTerrainGeometry(data, 0)
	grid := InsetForegroundGrid(data.Foreground)
	for y := .00125; y < 1; y += 0.013 {
		for x := 0.01825; x < generationWidth-0.018; x += 0.013 {
			p := geom.V{X: x, Y: y}
			inside := false
			for _, cell := range grid {
				inside = inside || geom.InsidePolygon(p, cell.Polygon)
			}
			if h.Collision.Contains(p.Add(geom.V{Y: h.Top})) != inside {
				t.Fatalf("exact collision boundary disagrees with visual rock at %v", p)
			}
		}
	}
}
