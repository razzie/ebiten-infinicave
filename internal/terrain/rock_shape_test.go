package terrain

import (
	"math"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestBackgroundFootprintPadsAndJoinsSharedFormations(t *testing.T) {
	// The second rectangle is a branch; the third is a neighboring formation.
	grid := RockGrid{terrainRect(.2, .2, .3, .1), terrainRect(.4, .3, .1, .25), terrainRect(.63, .2, .1, .35)}
	f := newRockFootprint(grid)
	for _, p := range []geom.V{{X: .3, Y: .25}, {X: .45, Y: .5}, {X: .3, Y: .15}, {X: .565, Y: .4}} {
		if f.backgroundHeight(p) <= rockContourHeight {
			t.Fatalf("rear footprint lost shared rock, padding, or overlap at %v", p)
		}
	}
	for _, p := range []geom.V{{X: .3, Y: .6}, {X: .5, Y: .8}} {
		if f.backgroundHeight(p) >= rockContourHeight {
			t.Fatalf("rear footprint added unrelated rock at %v", p)
		}
	}
	for _, p := range []geom.V{{X: -.49, Y: .25}, {X: .05, Y: .25}, {X: .95, Y: .4}, {X: 1.49, Y: .4}} {
		if f.backgroundHeight(p) <= rockContourHeight {
			t.Fatalf("border padding is missing at %v", p)
		}
	}
	if d := f.distance(geom.V{X: .45, Y: .3}); math.Abs(d+.05) > 1e-10 {
		t.Fatalf("internal cell join became a formation boundary: distance %g", d)
	}
	if !math.IsInf(newRockFootprint(nil).distance(geom.V{}), 1) {
		t.Fatal("empty foreground acquired a background formation")
	}
}

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

func TestRockContourDoesNotLeaveTeethBeyondClampedRelief(t *testing.T) {
	// Branch relief has a solid core and a zero-height exterior. Interpolating
	// those endpoint heights can leave tips in the flat exterior, where later
	// gradient polishing cannot move them back onto the intended silhouette.
	height := func(p geom.V) float64 {
		return math.Max(0, .020-2*math.Max(0, p.Y-.18))
	}
	grid := contourRockGrid(RockGrid{terrainRect(.1, .1, .2, .2)}, height)
	area := 0.0
	for _, c := range grid {
		area += geom.PolygonArea(c.Polygon)
		for _, p := range c.Polygon {
			if p.Y > .186+1e-10 || height(p) < rockContourHeight-1e-10 {
				t.Fatalf("clipping left an unsupported tooth at %v: height %g", p, height(p))
			}
		}
	}
	if math.Abs(area-.0172) > 1e-10 {
		t.Fatalf("clipping changed the solid footprint: area=%g", area)
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
	// Nonlinear relief must cross at the actual contour on both the long
	// merged edge and its shorter neighboring edge.
	const want = .23
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
	// A crossing along the lip must sample the raised side throughout the
	// nonlinear solve, even when the guide itself evaluates to zero height.
	height = func(p geom.V) float64 {
		if p.Y <= .2 {
			return 0
		}
		return math.Max(0, .024-.16*(p.X-.1))
	}
	grid = contourRockGrid(RockGrid{terrainRect(.1, .2, .2, .1)}, height, []Guide{guide})
	if len(grid) != 1 || math.Abs(geom.PolygonArea(grid[0].Polygon)-.01) > 1e-8 {
		t.Fatal("contour solving crossed onto the guide's empty side")
	}
}

func TestRockRoughnessIsSmallDeterministicAndWorldAnchored(t *testing.T) {
	base := RockGrid{terrainRect(.2, .2, .2, .2), terrainRect(.4, .2, .2, .2)}
	makeGrid := func(top float64, opts ShapeOptions) RockGrid {
		grid := RockGrid{terrainRect(.2, .2-top, .2, .2), terrainRect(.4, .2-top, .2, .2)}
		roughenRockEdges(grid, nil, 42, top, opts)
		return grid
	}
	opts := ShapeOptions{EdgeRoughness: .75, SpikeDensity: .65}
	a, b := makeGrid(0, opts), makeGrid(0, opts)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("edge detail changes on regeneration")
	}
	if !reflect.DeepEqual(base, makeGrid(0, ShapeOptions{})) {
		t.Fatal("zero edge settings changed geometry")
	}
	shifted := makeGrid(-1, opts)
	total := 0
	for i, c := range a {
		if len(c.Polygon) != len(shifted[i].Polygon) {
			t.Fatal("detail depends on section window")
		}
		for j, p := range c.Polygon {
			if p.Sub(shifted[i].Polygon[j].Add(geom.V{Y: -1})).Len() > 1e-9 {
				t.Fatal("detail moved between windows")
			}
			d := math.Inf(1)
			for k, q := range base[i].Polygon {
				d = math.Min(d, geom.SegmentDistanceSquared(p, p, q, base[i].Polygon[(k+1)%len(base[i].Polygon)]))
			}
			if math.Sqrt(d) > .006001 {
				t.Fatalf("oversized tooth: %g", math.Sqrt(d))
			}
			if p.X == .4 && p.Y > .200001 && p.Y < .399999 {
				t.Fatal("shared face edge acquired detail")
			}
		}
		if !simpleRockOutline(c.Polygon) || len(geom.Triangulate(c.Polygon)) != len(c.Polygon)-2 {
			t.Fatal("invalid chipped polygon")
		}
		total += len(c.Polygon)
	}
	if total < 40 {
		t.Fatalf("expected many shallow teeth, got %d vertices", total)
	}
}

func TestRockSpikeDensityKeepsSurvivingTeeth(t *testing.T) {
	makeGrid := func(density float64) RockGrid {
		grid := RockGrid{terrainRect(.2, .2, .3, .3)}
		roughenRockEdges(grid, nil, 42, 0, ShapeOptions{SpikeDensity: density})
		return grid
	}
	zero, low, high := makeGrid(0), makeGrid(.25), makeGrid(1)
	if len(zero[0].Polygon) != 4 {
		t.Fatal("disabled teeth changed edge")
	}
	if len(low[0].Polygon) != len(high[0].Polygon) {
		t.Fatal("density changed candidate placement")
	}
	for i, p := range low[0].Polygon {
		// A retained tooth has exactly the same geometry at higher density.
		onBorder := math.Min(math.Min(math.Abs(p.X-.2), math.Abs(p.X-.5)), math.Min(math.Abs(p.Y-.2), math.Abs(p.Y-.5))) < 1e-10
		if !onBorder && p != high[0].Polygon[i] {
			t.Fatal("surviving tooth changed shape")
		}
	}
}

func TestRockRoughnessReplacesTallNeedlesWithSmallTeeth(t *testing.T) {
	poly := []geom.V{{X: .2, Y: .2}, {X: .4, Y: .2}, {X: .4, Y: .294}, {X: .425, Y: .3}, {X: .4, Y: .306}, {X: .4, Y: .4}, {X: .2, Y: .4}}
	grid := RockGrid{terrainRock(poly)}
	roughenRockEdges(grid, nil, 42, 0, DefaultShapeOptions())
	for _, p := range grid[0].Polygon {
		if p.X > .410 {
			t.Fatalf("tall needle survived: %v", p)
		}
	}
	if len(grid[0].Polygon) <= len(poly) {
		t.Fatal("smaller teeth were not added")
	}
}
