package terrain

import (
	"image/color"
	"math"
	"math/rand"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestGuideCutsCellsWithoutAddingSites(t *testing.T) {
	seeds := []geom.V{{X: 0.25, Y: 0.48}, {X: 0.75, Y: 0.48}, {X: 0.25, Y: 0.8}, {X: 0.75, Y: 0.8}}
	guides := []Guide{SplineGuide([]geom.V{{X: -0.01, Y: 0.5}, {X: generationWidth + 0.01, Y: 0.5}}, 1)}
	grid := newGuideRockGrid(seeds, guides, func(p geom.V) color.NRGBA {
		if p.Y > .500 {
			return color.NRGBA{R: 200, A: 255}
		}
		return color.NRGBA{R: 20, A: 255}
	})
	if len(grid) != 6 {
		t.Fatalf("want only the two crossed cells split, got %d faces", len(grid))
	}
	area, seam := 0.0, 0.0
	for _, cell := range grid {
		area += geom.PolygonArea(cell.Polygon)
		for i, p := range cell.Polygon {
			if (cell.Center.Y-.500)*(p.Y-.500) < -1e-13 {
				t.Fatal("cell crosses guide border")
			}
			q := cell.Polygon[(i+1)%len(cell.Polygon)]
			if math.Abs(p.Y-.500) < 1e-10 && math.Abs(q.Y-.500) < 1e-10 {
				seam += p.Sub(q).Len()
			}
		}
		if cell.Center.Y > .500 && cell.Color.R != 200 {
			t.Fatal("lit fragment inherited the original shadow site's color")
		}
	}
	if math.Abs(area-generationWidth*GenerationHeight) > 1e-12 || math.Abs(seam-2*generationWidth) > 1e-9 {
		t.Fatalf("faces leave gaps or fail to share the guide: area=%v seam=%v", area, seam)
	}
}

func TestCurvedGuideCutsPreserveAreaAndCurve(t *testing.T) {
	poly := []geom.V{{X: 0.1, Y: 0.1}, {X: 0.3, Y: 0.1}, {X: 0.3, Y: 0.3}, {X: 0.1, Y: 0.3}}
	g := SplineGuide([]geom.V{{X: 0.08, Y: 0.18}, {X: 0.16, Y: 0.14}, {X: 0.24, Y: 0.25}, {X: 0.32, Y: 0.21}}, 1)
	faces := splitGuideFace(poly, &g)
	if len(faces) != 2 {
		t.Fatalf("curve produced %d faces, want two", len(faces))
	}
	assertFacesPartition(t, poly, faces)
	for _, p := range g.Pts {
		if !geom.InsidePolygon(p, poly) {
			continue
		}
		for _, face := range faces {
			found := false
			for i, a := range face {
				d := face[(i+1)%len(face)].Sub(a)
				q := a.Add(d.Mul(geom.Clamp(p.Sub(a).Dot(d)/d.Len2(), 0, 1)))
				found = found || p.Sub(q).Len() < 1e-10
			}
			if !found {
				t.Fatalf("curved border was replaced by a tangent or chord at %v", p)
			}
		}
	}
}

func TestGuideCrossingsAndFiniteEndpoints(t *testing.T) {
	poly := []geom.V{{X: 0.1, Y: 0.1}, {X: 0.3, Y: 0.1}, {X: 0.3, Y: 0.3}, {X: 0.1, Y: 0.3}}
	for _, tc := range []struct {
		name  string
		knots []geom.V
		count int
	}{
		{"curl", []geom.V{{X: 0.08, Y: 0.15}, {X: 0.18, Y: 0.15}, {X: 0.33, Y: 0.17}, {X: 0.33, Y: 0.24}, {X: 0.18, Y: 0.25}, {X: 0.08, Y: 0.25}}, 3},
		{"interior tip", []geom.V{{X: 0.08, Y: 0.2}, {X: 0.18, Y: 0.2}}, 1},
		{"outside segment", []geom.V{{X: 0.02, Y: 0.2}, {X: 0.08, Y: 0.2}}, 1},
		{"boundary", []geom.V{{X: 0.08, Y: 0.1}, {X: 0.32, Y: 0.1}}, 1},
		{"vertices", []geom.V{{X: 0.08, Y: 0.08}, {X: 0.32, Y: 0.32}}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := SplineGuide(tc.knots, 1)
			faces := splitGuideFace(poly, &g)
			if len(faces) != tc.count {
				t.Fatalf("got %d faces, want %d", len(faces), tc.count)
			}
			assertFacesPartition(t, poly, faces)
		})
	}
	a := SplineGuide([]geom.V{{X: 0.08, Y: 0.2}, {X: 0.32, Y: 0.2}}, 1)
	b := SplineGuide([]geom.V{{X: 0.2, Y: 0.08}, {X: 0.2, Y: 0.32}}, 1)
	var faces [][]geom.V
	for _, face := range splitGuideFace(poly, &a) {
		faces = append(faces, splitGuideFace(face, &b)...)
	}
	if len(faces) != 4 {
		t.Fatalf("intersecting guides produced %d faces, want four", len(faces))
	}
	assertFacesPartition(t, poly, faces)
}

func assertFacesPartition(t *testing.T, original []geom.V, faces [][]geom.V) {
	t.Helper()
	area := 0.0
	for _, face := range faces {
		area += geom.PolygonArea(face)
		if !geom.InsidePolygon(geom.PolygonCenter(face), face) {
			t.Fatal("face color sample lies outside the face")
		}
		triangles := geom.Triangulate(face)
		if len(triangles) != len(face)-2 {
			t.Fatalf("cannot triangulate face: %d triangles for %d vertices", len(triangles), len(face))
		}
		triArea := 0.0
		for _, tri := range triangles {
			triArea += geom.PolygonArea([]geom.V{face[tri[0]], face[tri[1]], face[tri[2]]})
		}
		if math.Abs(triArea-geom.PolygonArea(face)) > 1e-12 {
			t.Fatal("triangulation loses or overlaps face area")
		}
	}
	if math.Abs(area-geom.PolygonArea(original)) > 1e-12 {
		t.Fatalf("cut loses or overlaps area: %v vs %v", area, geom.PolygonArea(original))
	}
	for y := 0.1013; y < 0.3; y += 0.007 {
		for x := 0.1017; x < 0.3; x += 0.007 {
			count := 0
			for _, face := range faces {
				if geom.InsidePolygon(geom.V{X: x, Y: y}, face) {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("point %v belongs to %d faces", geom.V{X: x, Y: y}, count)
			}
		}
	}
}

func TestGuideNormalsRemainContinuous(t *testing.T) {
	g := SplineGuide([]geom.V{{X: 0.1, Y: 0.4}, {X: 0.24, Y: 0.4}, {X: 0.28, Y: 0.44}, {X: 0.24, Y: 0.49}, {X: 0.15, Y: 0.5}}, 1)
	for _, s := range g.S[1 : len(g.S)-1] {
		_, _, before := g.frameAt(s - 1e-8)
		_, _, after := g.frameAt(s + 1e-8)
		if before.Dot(after) < .99999 {
			t.Errorf("normal jumps at arc length %v", s)
		}
	}
}

func TestVineTerrainPreservesConcaveGuideCut(t *testing.T) {
	poly := []geom.V{{X: 0.1, Y: 0.1}, {X: 0.3, Y: 0.1}, {X: 0.3, Y: 0.3}, {X: 0.24, Y: 0.3}, {X: 0.24, Y: 0.16}, {X: 0.16, Y: 0.16}, {X: 0.16, Y: 0.3}, {X: 0.1, Y: 0.3}}
	background := RockGrid{{Center: geom.V{X: 0.5, Y: 1.5}, Polygon: []geom.V{{X: 0, Y: GenerationMinY}, {X: generationWidth, Y: GenerationMinY}, {X: generationWidth, Y: generationMaxY}, {X: 0, Y: generationMaxY}}, Color: color.NRGBA{R: 30, G: 30, B: 30, A: 255}}}
	foreground := RockGrid{{Center: geom.PolygonCenter(poly), Polygon: poly, Color: color.NRGBA{R: 200, G: 200, B: 200, A: 255}}}
	field := newVineTerrain(background, foreground)
	at := func(x, y float64) int {
		return int((y-GenerationMinY)/vineFieldStep)*vineFieldWidth + int(x/vineFieldStep)
	}
	if field.unsupported[at(.200, .260)] != 0 {
		t.Fatal("concave guide cut filled the dark passage between the rock arms")
	}
	if field.unsupported[at(.130, .260)] == 0 || field.unsupported[at(.270, .260)] == 0 {
		t.Fatal("vine terrain omitted a lit arm of the concave face")
	}
}

func TestTinyGuideFragmentsMergeOnSameSide(t *testing.T) {
	for _, height := range []float64{.001, .003} {
		// The 1px fragment is small in area; the 3px one is long and thin.
		// Both share only part of the upper neighbor's edge. The lower
		// neighbor comes first to catch accidental merging across the guide.
		polys := [][]geom.V{
			{{X: 0.1, Y: 0.2 - height}, {X: 0.16, Y: 0.2 - height}, {X: 0.16, Y: 0.2}, {X: 0.1, Y: 0.2}},
			{{X: 0.1, Y: 0.2}, {X: 0.2, Y: 0.2}, {X: 0.2, Y: 0.24}, {X: 0.1, Y: 0.24}},
			{{X: 0.1, Y: 0.16}, {X: 0.2, Y: 0.16}, {X: 0.2, Y: 0.2 - height}, {X: 0.1, Y: 0.2 - height}},
		}
		var faces []guideFragment
		area := 0.0
		for _, poly := range polys {
			faces = append(faces, makeGuideFragment(poly, geom.PolygonCenter(poly), true))
			area += geom.PolygonArea(poly)
		}
		guide := SplineGuide([]geom.V{{X: 0.09, Y: 0.2}, {X: 0.21, Y: 0.2}}, 1)
		mergeGuideFragments(faces, []Guide{guide})
		if faces[0].poly != nil || !reflect.DeepEqual(faces[1].poly, polys[1]) {
			t.Fatal("tiny fragment survived or merged across the guide")
		}
		if faces[2].tiny() || len(geom.Triangulate(faces[2].poly)) != len(faces[2].poly)-2 {
			t.Fatal("merged face is still tiny or cannot be rendered")
		}
		if math.Abs(geom.PolygonArea(faces[1].poly)+geom.PolygonArea(faces[2].poly)-area) > 1e-13 {
			t.Fatal("merge left a gap or overlapping geometry")
		}
		for _, p := range faces[2].poly {
			if p.Y > .200+1e-10 {
				t.Fatal("merge crossed the guide")
			}
		}
		for _, p := range []geom.V{{X: 0.13, Y: 0.1995}, {X: 0.13, Y: 0.18}, {X: 0.18, Y: 0.18}} {
			if !geom.InsidePolygon(p, faces[2].poly) {
				t.Fatalf("merged face lost part of its inputs at %v", p)
			}
		}
		if geom.InsidePolygon(geom.V{X: 0.18, Y: 0.1995}, faces[2].poly) {
			t.Fatal("union filled the concave notch outside its inputs")
		}
	}
}

func TestFragmentMergePreservesOrdinaryCellsAndProtectedBorders(t *testing.T) {
	a := []geom.V{{X: 0.1, Y: 0.199}, {X: 0.12, Y: 0.199}, {X: 0.12, Y: 0.2}, {X: 0.1, Y: 0.2}}
	b := []geom.V{{X: 0.1, Y: 0.2}, {X: 0.14, Y: 0.2}, {X: 0.14, Y: 0.24}, {X: 0.1, Y: 0.24}}
	for _, cut := range []bool{false, true} {
		poly := a
		if !cut {
			// Small but compact ordinary cells need no consolidation.
			poly = []geom.V{{X: 0.1, Y: 0.194}, {X: 0.106, Y: 0.194}, {X: 0.106, Y: 0.2}, {X: 0.1, Y: 0.2}}
		}
		faces := []guideFragment{makeGuideFragment(poly, geom.PolygonCenter(poly), cut), makeGuideFragment(b, geom.PolygonCenter(b), false)}
		g := SplineGuide([]geom.V{{X: 0.09, Y: 0.2}, {X: 0.15, Y: 0.2}}, 1)
		guides := []Guide(nil)
		if cut {
			guides = []Guide{g}
		}
		mergeGuideFragments(faces, guides)
		if !reflect.DeepEqual(faces[0].poly, poly) || !reflect.DeepEqual(faces[1].poly, b) {
			t.Fatal("changed an ordinary cell or erased a protected ridge")
		}
	}
}

func TestGeneratedGuideFragmentsMergeWithoutLosingArea(t *testing.T) {
	seed := int64(42)
	top := SectionTop(0)
	noise := NewPerlin(rand.New(rand.NewSource(seed)))
	noise.OffsetY = top
	guides := worldGuides(seed, 0)
	seeds := worldSeeds(seed, top, noise)
	// Measure the actual fragments created by clipping before consolidation.
	before := 0
	for i, site := range seeds {
		poly := voronoiCell(i, seeds)
		faces := [][]geom.V{poly}
		radius := 0.0
		for _, p := range poly {
			radius = math.Max(radius, p.Sub(site).Len())
		}
		for j := range guides {
			if guides[j].distanceBound2(site) > radius*radius {
				continue
			}
			var next [][]geom.V
			for _, face := range faces {
				next = append(next, splitGuideFace(face, &guides[j])...)
			}
			faces = next
		}
		if len(faces) > 1 {
			for _, face := range faces {
				if makeGuideFragment(face, geom.PolygonCenter(face), true).tiny() {
					before++
				}
			}
		}
	}
	grid := newGuideRockGrid(seeds, guides, func(geom.V) color.NRGBA { return color.NRGBA{A: 255} })
	after, area := 0, 0.0
	for _, face := range grid {
		area += geom.PolygonArea(face.Polygon)
		if makeGuideFragment(face.Polygon, face.Center, true).tiny() {
			after++
		}
		if !geom.InsidePolygon(face.Center, face.Polygon) || len(geom.Triangulate(face.Polygon)) != len(face.Polygon)-2 {
			t.Fatal("generated face is not renderable or samples outside its perimeter")
		}
	}
	t.Logf("tiny guide fragments: %d before merging, %d after", before, after)
	if before == 0 || after > before/10 {
		t.Fatalf("too many clipping slivers remain: %d of %d", after, before)
	}
	if math.Abs(area-generationWidth*GenerationHeight) > 1e-9 {
		t.Fatalf("merging changed the total terrain area: %v", area)
	}
}

func TestLongThinCellsMergeAtAnyScale(t *testing.T) {
	for _, cut := range []bool{false, true} {
		for _, scale := range []float64{1, 3} {
			// Area and absolute width both exceed the old cutoffs.
			polys := [][]geom.V{
				{{X: 0, Y: 0}, {X: 0.1, Y: 0}, {X: 0.1, Y: 0.008}, {X: 0, Y: 0.008}},
				{{X: 0, Y: 0.008}, {X: 0.1, Y: 0.008}, {X: 0.1, Y: 0.08}, {X: 0, Y: 0.08}},
			}
			var faces []guideFragment
			for _, poly := range polys {
				for i := range poly {
					// Rotate to ensure the criterion is not axis aligned.
					p := poly[i].Mul(scale)
					poly[i] = geom.V{X: 0.1 + (p.X-p.Y)/math.Sqrt2, Y: 0.1 + (p.X+p.Y)/math.Sqrt2}
				}
				faces = append(faces, makeGuideFragment(poly, geom.PolygonCenter(poly), cut))
			}
			mergeGuideFragments(faces, nil)
			if faces[0].poly != nil || faces[1].tiny() {
				t.Fatalf("long strip survived: cut=%v scale=%v", cut, scale)
			}
			if math.Abs(faces[1].area-.008*scale*scale) > 1e-13 || len(geom.Triangulate(faces[1].poly)) != len(faces[1].poly)-2 {
				t.Fatal("merge changed coverage or produced an unrenderable face")
			}
		}
	}
}

func TestThinCellChoosesBroaderUnion(t *testing.T) {
	// The lower neighbor shares more border, but the upper neighbor
	// produces a more compact union. Neither neighbor needs merging itself.
	polys := [][]geom.V{
		{{X: 0, Y: 0}, {X: 0.1, Y: 0}, {X: 0.1, Y: 0.008}, {X: 0, Y: 0.008}},
		{{X: 0, Y: -0.06}, {X: 0.28, Y: -0.06}, {X: 0.28, Y: 0}, {X: 0, Y: 0}},
		{{X: 0.01, Y: 0.008}, {X: 0.09, Y: 0.008}, {X: 0.09, Y: 0.038}, {X: 0.01, Y: 0.038}},
	}
	var faces []guideFragment
	for _, poly := range polys {
		faces = append(faces, makeGuideFragment(poly, geom.PolygonCenter(poly), true))
	}
	mergeGuideFragments(faces, nil)
	if faces[0].poly != nil || !reflect.DeepEqual(faces[1].poly, polys[1]) || math.Abs(faces[2].area-.0032) > 1e-12 {
		t.Fatal("thin cell did not choose the broader union")
	}
}

func TestThinCellCanMergeIntoSmallerNeighbor(t *testing.T) {
	polys := [][]geom.V{
		{{X: 0, Y: 0}, {X: 0.1, Y: 0}, {X: 0.1, Y: 0.008}, {X: 0, Y: 0.008}},
		{{X: 0.03, Y: 0.008}, {X: 0.07, Y: 0.008}, {X: 0.07, Y: 0.026}, {X: 0.03, Y: 0.026}},
	}
	var faces []guideFragment
	for _, poly := range polys {
		faces = append(faces, makeGuideFragment(poly, geom.PolygonCenter(poly), true))
	}
	mergeGuideFragments(faces, nil)
	if faces[0].poly != nil || math.Abs(faces[1].area-.00152) > 1e-12 {
		t.Fatal("thin cell survived despite having a suitable smaller neighbor")
	}
}

func TestThinCellsDoNotMergeIntoThinnerUnion(t *testing.T) {
	// Joining these strips end to end worsens compactness. Accepting this
	// merge lets an already thin face keep growing instead of repairing it.
	polys := [][]geom.V{
		{{X: 0, Y: 0}, {X: 0.1, Y: 0}, {X: 0.1, Y: 0.008}, {X: 0, Y: 0.008}},
		{{X: 0.1, Y: 0}, {X: 0.2, Y: 0}, {X: 0.2, Y: 0.008}, {X: 0.1, Y: 0.008}},
	}
	var faces []guideFragment
	for _, poly := range polys {
		faces = append(faces, makeGuideFragment(poly, geom.PolygonCenter(poly), false))
	}
	mergeGuideFragments(faces, nil)
	for i, face := range faces {
		if !reflect.DeepEqual(face.poly, polys[i]) {
			t.Fatal("merge made a thin face thinner")
		}
	}
}

func TestTaperedGuideCellMerges(t *testing.T) {
	// This wedge cleared the previous .30 compactness threshold even though
	// it is over three times as long as it is wide and ends in a sharp tip.
	polys := [][]geom.V{
		{{X: 0, Y: 0}, {X: 0.08, Y: 0}, {X: 0, Y: 0.024}},
		{{X: 0.08, Y: 0}, {X: 0.08, Y: 0.06}, {X: 0, Y: 0.06}, {X: 0, Y: 0.024}},
	}
	var faces []guideFragment
	for _, poly := range polys {
		faces = append(faces, makeGuideFragment(poly, geom.PolygonCenter(poly), true))
	}
	guide := SplineGuide([]geom.V{{X: 0, Y: 0}, {X: 0.08, Y: 0}}, 1)
	mergeGuideFragments(faces, []Guide{guide})
	if faces[0].poly != nil || faces[1].tiny() || math.Abs(faces[1].area-.0048) > 1e-13 {
		t.Fatal("tapered cell survived or merge lost coverage")
	}
}
