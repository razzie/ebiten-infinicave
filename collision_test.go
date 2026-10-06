package infinicave

import (
	"math"
	"reflect"
	"testing"
)

func TestCollisionUnionWithHoleAndPartialSharedBorders(t *testing.T) {
	// Four strips enclose a hole. Their shared edges have different lengths.
	grid := RockGrid{
		hoverRect(30, 1100, 80, 20), hoverRect(30, 1160, 80, 20),
		hoverRect(30, 1120, 20, 40), hoverRect(90, 1120, 20, 40),
		// A corner-touching rectangle stays a separate block.
		hoverRect(110, 1180, 20, 20),
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
	if math.Abs(area-5200) > 1e-6 || holes != 1 {
		t.Fatalf("area %v / holes %d; internal borders or hole are incorrect", area, holes)
	}
	for _, p := range []V{{40, -890}, {100, -850}, {40, -840}, {30, -880}, {120, -810}} {
		if !geometry.Contains(p) {
			t.Fatalf("solid point %v was missed", p)
		}
	}
	for _, p := range []V{{70, -850}, {20, -850}, {140, -850}} {
		if geometry.Contains(p) {
			t.Fatalf("empty point %v was filled", p)
		}
	}
	if got := prepareTerrainGeometry(sectionData{foreground: grid}, 0).collision; !reflect.DeepEqual(got, geometry) {
		t.Fatal("collision contour ordering is nondeterministic")
	}
	for _, tolerance := range []float64{1, 10, 1000} {
		g := prepareTerrainGeometry(sectionData{foreground: grid}, tolerance).collision
		if g.Contains(V{70, -850}) || !g.Contains(V{40, -850}) {
			t.Fatal("simplification lost the hole or its enclosing rock")
		}
	}
}

func TestCollisionSimplificationToleranceAndSeams(t *testing.T) {
	poly := []V{{30, 980}, {40, 980.1}, {50, 980}, {60, 980.1}, {70, 980},
		{70, 1020}, {60, 1020.1}, {50, 1020}, {40, 1020.1}, {30, 1020}}
	before := append([]V(nil), poly...)
	simplified := simplifyCollisionLoop(poly, .2)
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
		if best > .2*.2+1e-9 {
			t.Fatalf("vertex %v exceeds tolerance: %v", p, math.Sqrt(best))
		}
	}
	for _, seam := range []V{{30, 1000}, {70, 1000}} {
		found := false
		for _, p := range simplified {
			found = found || p == seam
		}
		if !found {
			t.Fatalf("exact section crossing %v was removed", seam)
		}
	}
	// Deep concavities and narrow features must never become self-crossing.
	concave := []V{{30, 1100}, {70, 1100}, {70, 1110}, {40, 1110}, {40, 1140}, {30, 1140}}
	for _, tolerance := range []float64{1, 10, 1000} {
		g := simplifyCollisionLoop(concave, tolerance)
		if len(g) < 3 || !simpleCollisionLoop(g) || faceArea(g) <= 0 {
			t.Fatal("simplification produced an invalid polygon")
		}
	}
}

func TestSceneCollisionGeometryOwnershipAndAvailability(t *testing.T) {
	h := prepareTerrainGeometry(sectionData{foreground: RockGrid{hoverRect(30, 1100, 30, 30)}}, 0)
	scene := &Scene{world: &world{sections: map[int64]*worldSection{0: {geometry: h}}}}
	geometry, ok := scene.CollisionGeometry(0)
	if !ok || len(geometry.Polygons) == 0 {
		t.Fatal("cached collision geometry is unavailable")
	}
	geometry.Polygons[0][0].X += 100
	if geometry.Polygons[0][0] == h.collision.Polygons[0][0] {
		t.Fatal("caller modified the cached collision geometry")
	}
	if _, ok := scene.CollisionGeometry(1); ok {
		t.Fatal("missing section reported ready")
	}
	scene.closed = true
	if _, ok := scene.CollisionGeometry(0); ok {
		t.Fatal("closed scene exposed stale geometry")
	}
}

func TestGeneratedCollisionMatchesRockFaces(t *testing.T) {
	data := buildSectionMode(42, 0, StudyCurl)
	h := prepareTerrainGeometry(data, 0)
	grid := insetForegroundGrid(data.foreground)
	for y := 1001.25; y < 2000; y += 13 {
		for x := 18.25; x < W-18; x += 13 {
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
