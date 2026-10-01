package main

import (
	"math"
	"testing"
)

func TestRockContourTrimsFacesWithoutLightWedges(t *testing.T) {
	poly := []V{{100, 100}, {200, 100}, {200, 200}, {100, 200}}
	c := RockCell{Center: V{150, 150}, Polygon: poly, Z: 30, Raised: true, Normal: (V3{.3, -.5, 1}).Norm()}
	grid := contourRockGrid(RockGrid{c}, func(p V) float64 { return rockContourHeight + 150 - p.X })
	if len(grid) != 1 {
		t.Fatalf("one connected clipped face became %d pieces", len(grid))
	}
	if math.Abs(faceArea(grid[0].Polygon)-5000) > .01 {
		t.Fatalf("contour did not bisect the face: area=%v", faceArea(grid[0].Polygon))
	}
	if grid[0].Normal != c.Normal || !insideFace(grid[0].Center, grid[0].Polygon) {
		t.Fatal("clipping changed the shading plane or left its control point outside")
	}
	for _, p := range grid[0].Polygon {
		if p.X > 150.001 {
			t.Fatal("contour retains an entire outer Voronoi cell")
		}
	}
}

func TestRockContourPreservesConcaveGap(t *testing.T) {
	poly := []V{{100, 100}, {300, 100}, {300, 300}, {240, 300}, {240, 160}, {160, 160}, {160, 300}, {100, 300}}
	faces := contourRockFace(poly, faceCenter(poly), func(p V) float64 { return rockContourHeight + p.Y - 200 })
	area := 0.0
	for _, face := range faces {
		area += faceArea(face)
		if insideFace(V{200, 250}, face) {
			t.Fatal("clipping bridged the concave recess")
		}
		if len(faceTriangles(face)) != len(face)-2 {
			t.Fatal("clipped face cannot be triangulated")
		}
	}
	if len(faces) != 2 || math.Abs(area-12000) > .1 {
		t.Fatalf("expected two separated rock arms, got %d faces and area %v", len(faces), area)
	}
}

func TestRockContourFindsInteriorRelief(t *testing.T) {
	poly := []V{{100, 100}, {200, 100}, {200, 200}, {100, 200}}
	faces := contourRockFace(poly, V{150, 150}, func(p V) float64 { return 30 - p.Sub(V{150, 150}).Len() })
	if len(faces) != 1 || !insideFace(V{150, 150}, faces[0]) {
		t.Fatal("relief entirely inside a face was lost")
	}
}

func TestArtisticFacetSelectionMatchesOverlappingWindows(t *testing.T) {
	a := []Guide{splineGuide([]V{{100, 400}, {900, 400}}, 1)}
	b := []Guide{splineGuide([]V{{100, 1400}, {900, 1400}}, 1)}
	var sitesA, sitesB []V
	for y := 410.0; y < 650; y += 25 {
		for x := 125.0; x < 875; x += 25 {
			sitesA = append(sitesA, V{x, y})
			sitesB = append(sitesB, V{x, y + 1000})
		}
	}
	left, right := artisticRockSeeds(sitesA, a, 42, 0), artisticRockSeeds(sitesB, b, 42, -1000)
	if len(left) >= len(sitesA) || len(left) != len(right) {
		t.Fatal("flank facet selection is not stable or has no effect")
	}
	for i, p := range left {
		q := right[i]
		q.Y -= 1000
		if p.Sub(q).Len() > 1e-9 {
			t.Fatal("facet placement changes across windows")
		}
	}
}

func TestContourPolishKeepsSharedJunctionsTogether(t *testing.T) {
	grid := RockGrid{
		{Center: V{125, 150}, Polygon: []V{{100, 100}, {150, 100}, {150, 205}, {100, 195}}},
		{Center: V{175, 150}, Polygon: []V{{150, 100}, {200, 100}, {200, 195}, {150, 205}}},
	}
	polishRockContours(grid, nil, func(p V) float64 { return rockContourHeight + 200 - p.Y })
	if math.Abs(mergeableBorder(grid[0].Polygon, grid[1].Polygon, nil)-100) > 1e-6 {
		t.Fatal("contour polishing cracked the shared junction")
	}
	for _, c := range grid {
		if !simpleRockOutline(c.Polygon) || len(faceTriangles(c.Polygon)) != len(c.Polygon)-2 {
			t.Fatal("contour polishing folded a face")
		}
		for _, p := range c.Polygon {
			if p.Y > 180 && math.Abs(p.Y-200) > .25 {
				t.Fatalf("sampling tooth remains at %v", p)
			}
		}
	}
}
