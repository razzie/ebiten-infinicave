package terrain

import (
	"math"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestRockContourTrimsFacesWithoutLightWedges(t *testing.T) {
	poly := []geom.V{{X: 0.1, Y: 0.1}, {X: 0.2, Y: 0.1}, {X: 0.2, Y: 0.2}, {X: 0.1, Y: 0.2}}
	c := RockCell{Center: geom.V{X: 0.15, Y: 0.15}, Polygon: poly, Z: 0.03, Raised: true, Normal: (geom.V3{X: .3, Y: -.5, Z: 1}).Norm()}
	grid := contourRockGrid(RockGrid{c}, func(p geom.V) float64 { return rockContourHeight + .150 - p.X })
	if len(grid) != 1 {
		t.Fatalf("one connected clipped face became %d pieces", len(grid))
	}
	if math.Abs(geom.PolygonArea(grid[0].Polygon)-.005) > 1e-12 {
		t.Fatalf("contour did not bisect the face: area=%v", geom.PolygonArea(grid[0].Polygon))
	}
	if grid[0].Normal != c.Normal || !geom.InsidePolygon(grid[0].Center, grid[0].Polygon) {
		t.Fatal("clipping changed the shading plane or left its control point outside")
	}
	for _, p := range grid[0].Polygon {
		if p.X > .150001 {
			t.Fatal("contour retains an entire outer Voronoi cell")
		}
	}
}

func TestRockContourPreservesConcaveGap(t *testing.T) {
	poly := []geom.V{{X: 0.1, Y: 0.1}, {X: 0.3, Y: 0.1}, {X: 0.3, Y: 0.3}, {X: 0.24, Y: 0.3}, {X: 0.24, Y: 0.16}, {X: 0.16, Y: 0.16}, {X: 0.16, Y: 0.3}, {X: 0.1, Y: 0.3}}
	faces := contourRockFace(poly, geom.PolygonCenter(poly), func(p geom.V) float64 { return rockContourHeight + p.Y - .200 })
	area := 0.0
	for _, face := range faces {
		area += geom.PolygonArea(face)
		if geom.InsidePolygon(geom.V{X: 0.2, Y: 0.25}, face) {
			t.Fatal("clipping bridged the concave recess")
		}
		if len(geom.Triangulate(face)) != len(face)-2 {
			t.Fatal("clipped face cannot be triangulated")
		}
	}
	if len(faces) != 2 || math.Abs(area-.012) > 1e-7 {
		t.Fatalf("expected two separated rock arms, got %d faces and area %v", len(faces), area)
	}
}

func TestRockContourFindsInteriorRelief(t *testing.T) {
	poly := []geom.V{{X: 0.1, Y: 0.1}, {X: 0.2, Y: 0.1}, {X: 0.2, Y: 0.2}, {X: 0.1, Y: 0.2}}
	faces := contourRockFace(poly, geom.V{X: 0.15, Y: 0.15}, func(p geom.V) float64 { return .030 - p.Sub(geom.V{X: 0.15, Y: 0.15}).Len() })
	if len(faces) != 1 || !geom.InsidePolygon(geom.V{X: 0.15, Y: 0.15}, faces[0]) {
		t.Fatal("relief entirely inside a face was lost")
	}
}

func TestContourPolishKeepsSharedJunctionsTogether(t *testing.T) {
	grid := RockGrid{
		{Center: geom.V{X: 0.125, Y: 0.15}, Polygon: []geom.V{{X: 0.1, Y: 0.1}, {X: 0.15, Y: 0.1}, {X: 0.15, Y: 0.205}, {X: 0.1, Y: 0.195}}},
		{Center: geom.V{X: 0.175, Y: 0.15}, Polygon: []geom.V{{X: 0.15, Y: 0.1}, {X: 0.2, Y: 0.1}, {X: 0.2, Y: 0.195}, {X: 0.15, Y: 0.205}}},
	}
	polishRockContours(grid, nil, func(p geom.V) float64 { return rockContourHeight + .200 - p.Y })
	if math.Abs(mergeableBorder(grid[0].Polygon, grid[1].Polygon, nil)-.100) > 1e-9 {
		t.Fatal("contour polishing cracked the shared junction")
	}
	for _, c := range grid {
		if !simpleRockOutline(c.Polygon) || len(geom.Triangulate(c.Polygon)) != len(c.Polygon)-2 {
			t.Fatal("contour polishing folded a face")
		}
		for _, p := range c.Polygon {
			if p.Y > .180 && math.Abs(p.Y-.200) > .00025 {
				t.Fatalf("sampling tooth remains at %v", p)
			}
		}
	}
}

func TestContourPolishPreservesPartialSharedEdge(t *testing.T) {
	// The corner at (.2, .2) lies inside the left face's long edge. Moving
	// only the right face's vertex would tear open a highlighted interior seam.
	grid := RockGrid{terrainRect(.1, .1, .1, .16), terrainRect(.2, .1, .06, .1)}
	polishRockContours(grid, nil, func(p geom.V) float64 { return rockContourHeight + .2 - p.Y })
	if shared := mergeableBorder(grid[0].Polygon, grid[1].Polygon, nil); shared < .09 {
		t.Fatalf("polishing tore the partial shared edge: shared=%g", shared)
	}
	moved := false
	for _, p := range grid[1].Polygon {
		if math.Abs(p.Y-.2) < 1e-9 && p.X > .201 && p.X < .21 {
			moved = true
			found := false
			for _, q := range grid[0].Polygon {
				if p.Sub(q).Len() < mergeTolerance {
					found = true
				}
			}
			if !found {
				t.Fatal("the moved junction is missing from its neighboring face")
			}
		}
	}
	if !moved {
		t.Fatal("fixture did not move the shared contour junction")
	}
}

func TestContourSamplesContinuousSharedVertices(t *testing.T) {
	grid := RockGrid{terrainRect(.1, .1, .1, .1), terrainRect(.2, .1, .1, .14)}
	height := func(p geom.V) float64 { return rockContourHeight + .16 - p.Y }
	clipped := contourRockGrid(grid, height)
	if len(clipped) != 2 {
		t.Fatalf("expected two clipped neighbors, got %d", len(clipped))
	}
	if shared := mergeableBorder(clipped[0].Polygon, clipped[1].Polygon, nil); math.Abs(shared-.06) > 1e-12 {
		t.Fatalf("continuous height sampling cracked the shared edge: %g", shared)
	}
	for _, c := range clipped {
		for _, p := range c.Polygon {
			if p.Y > .14 && math.Abs(p.Y-.16) > 1e-12 {
				t.Fatalf("contour crossing depends on the face's center: %v", p)
			}
		}
	}
}

func TestContourSamplesPartialSharedEdgesAtSameJunctions(t *testing.T) {
	grid := RockGrid{
		terrainRect(.1, .1, .1, .2),
		terrainRect(.2, .1, .1, .1),
		terrainRect(.2, .2, .1, .1),
	}
	height := func(p geom.V) float64 { return rockContourHeight + .23*.23 - p.Y*p.Y }
	clipped := contourRockGrid(grid, height)
	if len(clipped) != 3 {
		t.Fatalf("expected three clipped neighbors, got %d", len(clipped))
	}
	// Nonlinear relief must still interpolate through the same junctions on
	// the long merged edge and both shorter neighboring edges.
	want := .2 + (.23*.23-.2*.2)/(.3*.3-.2*.2)*.1
	for _, i := range []int{0, 2} {
		found := false
		for _, p := range clipped[i].Polygon {
			if math.Abs(p.X-.2) < 1e-12 && math.Abs(p.Y-want) < 1e-12 {
				found = true
			}
		}
		if !found {
			t.Fatalf("face %d has a different shared contour crossing", i)
		}
	}
}

func TestContourKeepsGuideHeightDiscontinuity(t *testing.T) {
	guide := SplineGuide([]geom.V{{X: .1, Y: .2}, {X: .3, Y: .2}}, 1)
	height := func(p geom.V) float64 {
		if p.Y > .2 {
			return .04
		}
		return 0
	}
	grid := contourRockGrid(RockGrid{terrainRect(.1, .2, .2, .1)}, height, []Guide{guide})
	if len(grid) != 1 || math.Abs(geom.PolygonArea(grid[0].Polygon)-.02) > 1e-12 {
		t.Fatal("sampling on the guide eroded its raised side")
	}
}
