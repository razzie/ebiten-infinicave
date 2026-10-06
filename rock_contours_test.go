package infinicave

import (
	"math"
	"testing"
)

func TestRockContourTrimsFacesWithoutLightWedges(t *testing.T) {
	poly := []V{{0.1, 0.1}, {0.2, 0.1}, {0.2, 0.2}, {0.1, 0.2}}
	c := RockCell{Center: V{0.15, 0.15}, Polygon: poly, Z: 0.03, Raised: true, Normal: (V3{.3, -.5, 1}).Norm()}
	grid := contourRockGrid(RockGrid{c}, func(p V) float64 { return rockContourHeight + .150 - p.X })
	if len(grid) != 1 {
		t.Fatalf("one connected clipped face became %d pieces", len(grid))
	}
	if math.Abs(faceArea(grid[0].Polygon)-.005) > 1e-12 {
		t.Fatalf("contour did not bisect the face: area=%v", faceArea(grid[0].Polygon))
	}
	if grid[0].Normal != c.Normal || !insideFace(grid[0].Center, grid[0].Polygon) {
		t.Fatal("clipping changed the shading plane or left its control point outside")
	}
	for _, p := range grid[0].Polygon {
		if p.X > .150001 {
			t.Fatal("contour retains an entire outer Voronoi cell")
		}
	}
}

func TestRockContourPreservesConcaveGap(t *testing.T) {
	poly := []V{{0.1, 0.1}, {0.3, 0.1}, {0.3, 0.3}, {0.24, 0.3}, {0.24, 0.16}, {0.16, 0.16}, {0.16, 0.3}, {0.1, 0.3}}
	faces := contourRockFace(poly, faceCenter(poly), func(p V) float64 { return rockContourHeight + p.Y - .200 })
	area := 0.0
	for _, face := range faces {
		area += faceArea(face)
		if insideFace(V{0.2, 0.25}, face) {
			t.Fatal("clipping bridged the concave recess")
		}
		if len(faceTriangles(face)) != len(face)-2 {
			t.Fatal("clipped face cannot be triangulated")
		}
	}
	if len(faces) != 2 || math.Abs(area-.012) > 1e-7 {
		t.Fatalf("expected two separated rock arms, got %d faces and area %v", len(faces), area)
	}
}

func TestRockContourFindsInteriorRelief(t *testing.T) {
	poly := []V{{0.1, 0.1}, {0.2, 0.1}, {0.2, 0.2}, {0.1, 0.2}}
	faces := contourRockFace(poly, V{0.15, 0.15}, func(p V) float64 { return .030 - p.Sub(V{0.15, 0.15}).Len() })
	if len(faces) != 1 || !insideFace(V{0.15, 0.15}, faces[0]) {
		t.Fatal("relief entirely inside a face was lost")
	}
}

func TestArtisticFacetSelectionMatchesOverlappingWindows(t *testing.T) {
	a := []Guide{splineGuide([]V{{0.1, 0.4}, {0.9, 0.4}}, 1)}
	b := []Guide{splineGuide([]V{{0.1, 1.4}, {0.9, 1.4}}, 1)}
	var sitesA, sitesB []V
	for y := 0.41; y < 0.65; y += 0.025 {
		for x := 0.125; x < 0.875; x += 0.025 {
			sitesA = append(sitesA, V{x, y})
			sitesB = append(sitesB, V{x, y + 1})
		}
	}
	left, right := artisticRockSeeds(sitesA, a, 42, 0), artisticRockSeeds(sitesB, b, 42, -1)
	if len(left) >= len(sitesA) || len(left) != len(right) {
		t.Fatal("flank facet selection is not stable or has no effect")
	}
	for i, p := range left {
		q := right[i]
		q.Y -= 1
		if p.Sub(q).Len() > 1e-9 {
			t.Fatal("facet placement changes across windows")
		}
	}
}

func TestContourPolishKeepsSharedJunctionsTogether(t *testing.T) {
	grid := RockGrid{
		{Center: V{0.125, 0.15}, Polygon: []V{{0.1, 0.1}, {0.15, 0.1}, {0.15, 0.205}, {0.1, 0.195}}},
		{Center: V{0.175, 0.15}, Polygon: []V{{0.15, 0.1}, {0.2, 0.1}, {0.2, 0.195}, {0.15, 0.205}}},
	}
	polishRockContours(grid, nil, func(p V) float64 { return rockContourHeight + .200 - p.Y })
	if math.Abs(mergeableBorder(grid[0].Polygon, grid[1].Polygon, nil)-.100) > 1e-9 {
		t.Fatal("contour polishing cracked the shared junction")
	}
	for _, c := range grid {
		if !simpleRockOutline(c.Polygon) || len(faceTriangles(c.Polygon)) != len(c.Polygon)-2 {
			t.Fatal("contour polishing folded a face")
		}
		for _, p := range c.Polygon {
			if p.Y > .180 && math.Abs(p.Y-.200) > .00025 {
				t.Fatalf("sampling tooth remains at %v", p)
			}
		}
	}
}
