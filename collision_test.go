package infinicave

import (
	"math"
	"reflect"
	"testing"
)

func TestCollisionUnionWithHoleAndPartialSharedBorders(t *testing.T) {
	// Four strips enclose a hole. Their shared edges have different lengths.
	grid := RockGrid{
		hoverRect(0.03, 0.1, 0.08, 0.02), hoverRect(0.03, 0.16, 0.08, 0.02),
		hoverRect(0.03, 0.12, 0.02, 0.04), hoverRect(0.09, 0.12, 0.02, 0.04),
		// A corner-touching rectangle stays a separate block.
		hoverRect(0.11, 0.18, 0.02, 0.02),
	}
	geometry := prepareTerrainGeometry(sectionData{foreground: grid}, 0).collision
	if len(geometry.Polygons) != 3 {
		t.Fatalf("got %d contours, want outer boundary, hole, and separate rectangle", len(geometry.Polygons))
	}
	area, holes := 0.0, 0
	for _, poly := range geometry.Polygons {
		area += faceArea(poly)
		if faceArea(poly) < 0 {
			holes++
		}
	}
	if math.Abs(area-.0052) > 1e-12 || holes != 1 {
		t.Fatalf("area %v / holes %d; internal borders or hole are incorrect", area, holes)
	}
	for _, p := range []V{{0.04, -0.89}, {0.1, -0.85}, {0.04, -0.84}, {0.03, -0.88}, {0.12, -0.81}} {
		if !geometry.Contains(p) {
			t.Fatalf("solid point %v was missed", p)
		}
	}
	for _, p := range []V{{0.07, -0.85}, {0.02, -0.85}, {0.14, -0.85}} {
		if geometry.Contains(p) {
			t.Fatalf("empty point %v was filled", p)
		}
	}
	if got := prepareTerrainGeometry(sectionData{foreground: grid}, 0).collision; !reflect.DeepEqual(got, geometry) {
		t.Fatal("collision contour ordering is nondeterministic")
	}
	for _, tolerance := range []float64{.001, .010, 1} {
		g := prepareTerrainGeometry(sectionData{foreground: grid}, tolerance).collision
		if g.Contains(V{0.07, -0.85}) || !g.Contains(V{0.04, -0.85}) {
			t.Fatal("simplification lost the hole or its enclosing rock")
		}
	}
}

func TestCollisionSimplificationToleranceAndSeams(t *testing.T) {
	poly := []V{{0.03, 0.98}, {0.04, 0.9801}, {0.05, 0.98}, {0.06, 0.9801}, {0.07, 0.98},
		{0.07, 1.02}, {0.06, 1.0201}, {0.05, 1.02}, {0.04, 1.0201}, {0.03, 1.02}}
	before := append([]V(nil), poly...)
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
			best = math.Min(best, guideSegmentsDistance2(p, p, a, simplified[(i+1)%len(simplified)]))
		}
		if best > .0002*.0002+1e-15 {
			t.Fatalf("vertex %v exceeds tolerance: %v", p, math.Sqrt(best))
		}
	}
	for _, seam := range []V{{0.03, 1}, {0.07, 1}} {
		found := false
		for _, p := range simplified {
			found = found || p == seam
		}
		if !found {
			t.Fatalf("exact section crossing %v was removed", seam)
		}
	}
	// Deep concavities and narrow features must never become self-crossing.
	concave := []V{{0.03, 1.1}, {0.07, 1.1}, {0.07, 1.11}, {0.04, 1.11}, {0.04, 1.14}, {0.03, 1.14}}
	for _, tolerance := range []float64{.001, .010, 1} {
		g := simplifyCollisionLoop(concave, tolerance)
		if len(g) < 3 || !simpleCollisionLoop(g) || faceArea(g) <= 0 {
			t.Fatal("simplification produced an invalid polygon")
		}
	}
}

func TestSceneCollisionGeometryOwnershipAndAvailability(t *testing.T) {
	h := prepareTerrainGeometry(sectionData{foreground: RockGrid{hoverRect(0.03, 0.1, 0.03, 0.03)}}, 0)
	scene := &Scene{world: &world{sections: map[int64]*worldSection{0: {geometry: h}}}}
	geometry, ok := scene.CollisionGeometry(0)
	if !ok || len(geometry.Polygons) == 0 {
		t.Fatal("cached collision geometry is unavailable")
	}
	if geometry.Top != -1 || !geometry.Contains(V{.04, -.89}) || geometry.Contains(V{.4, -.89}) {
		t.Fatal("cached collision geometry does not use scene units")
	}
	before := h.collision.Polygons[0][0]
	geometry.Polygons[0][0].X += 100
	if h.collision.Polygons[0][0] != before {
		t.Fatal("caller modified the cached collision geometry")
	}
	copy, ok := scene.CollisionGeometry(0)
	if !ok || !copy.Contains(V{.04, -.89}) {
		t.Fatal("repeated collision access changed the cached geometry")
	}
	upper := prepareTerrainGeometry(sectionData{id: 1, foreground: RockGrid{hoverRect(0.03, 0.1, 0.03, 0.03)}}, 0)
	scene.world.sections[1] = &worldSection{geometry: upper}
	if geometry, ok := scene.CollisionGeometry(-1); !ok || geometry.ID != -1 || geometry.Top != -2 || !geometry.Contains(V{.04, -1.89}) {
		t.Fatal("negative section ID did not resolve the correct cached geometry")
	}
	if _, ok := scene.CollisionGeometry(1); ok {
		t.Fatal("positive section ID accepted")
	}
	if _, ok := scene.CollisionGeometry(-2); ok {
		t.Fatal("missing section reported ready")
	}
	scene.closed = true
	if _, ok := scene.CollisionGeometry(0); ok {
		t.Fatal("closed scene exposed stale geometry")
	}
}

func TestGeneratedCollisionMatchesRockFaces(t *testing.T) {
	data := buildSectionMode(42, 0, StudyCurl, nil)
	h := prepareTerrainGeometry(data, 0)
	grid := insetForegroundGrid(data.foreground)
	for y := .00125; y < 1; y += 0.013 {
		for x := 0.01825; x < generationWidth-0.018; x += 0.013 {
			p := V{x, y}
			inside := false
			for _, cell := range grid {
				inside = inside || insideFace(p, cell.Polygon)
			}
			if h.collision.Contains(p.Add(V{Y: h.top})) != inside {
				t.Fatalf("exact collision boundary disagrees with visual rock at %v", p)
			}
		}
	}
}
