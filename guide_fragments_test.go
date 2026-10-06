package infinicave

import (
	"image/color"
	"math"
	"math/rand"
	"reflect"
	"testing"
)

func TestRidgedGuideHasBroadDeterministicFacets(t *testing.T) {
	original := splineGuide([]V{{100, 200}, {500, 200}}, 1)
	g := ridgedGuide(original, 42)
	if !reflect.DeepEqual(g, ridgedGuide(original, 42)) {
		t.Fatal("ridge changes when regenerated")
	}
	if len(g.Pts) >= len(original.Pts)/2 || len(g.Pts) < 15 {
		t.Fatalf("expected broad facets instead of dense curve samples, got %d", len(g.Pts))
	}
	if g.Pts[0] != original.Pts[0] || g.Pts[len(g.Pts)-1] != original.Pts[len(original.Pts)-1] {
		t.Fatal("roughness moved a guide endpoint")
	}
	minY, maxY := 200.0, 200.0
	for _, p := range g.Pts {
		minY, maxY = math.Min(minY, p.Y), math.Max(maxY, p.Y)
		if math.Abs(p.Y-200) > 2.5 {
			t.Fatal("ridge no longer follows the original guide")
		}
	}
	if maxY-minY < 3 {
		t.Fatal("guide border is still smooth")
	}
	// Translating a regeneration window cannot change its geometry.
	original.translateY(-1000)
	shifted := ridgedGuide(original, 42)
	for i, p := range g.Pts {
		if p.Sub(shifted.Pts[i].Add(V{0, 1000})).Len() > 1e-9 {
			t.Fatal("roughness depends on section position")
		}
	}
}

func TestTinyGuideFragmentsMergeOnSameSide(t *testing.T) {
	for _, height := range []float64{1, 3} {
		// The 1px fragment is small in area; the 3px one is long and thin.
		// Both share only part of the upper neighbor's edge. The lower
		// neighbor comes first to catch accidental merging across the guide.
		polys := [][]V{
			{{100, 200 - height}, {160, 200 - height}, {160, 200}, {100, 200}},
			{{100, 200}, {200, 200}, {200, 240}, {100, 240}},
			{{100, 160}, {200, 160}, {200, 200 - height}, {100, 200 - height}},
		}
		var faces []guideFragment
		area := 0.0
		for _, poly := range polys {
			faces = append(faces, makeGuideFragment(poly, faceCenter(poly), true))
			area += faceArea(poly)
		}
		guide := splineGuide([]V{{90, 200}, {210, 200}}, 1)
		mergeGuideFragments(faces, []Guide{guide})
		if faces[0].poly != nil || !reflect.DeepEqual(faces[1].poly, polys[1]) {
			t.Fatal("tiny fragment survived or merged across the guide")
		}
		if faces[2].tiny() || len(faceTriangles(faces[2].poly)) != len(faces[2].poly)-2 {
			t.Fatal("merged face is still tiny or cannot be rendered")
		}
		if math.Abs(faceArea(faces[1].poly)+faceArea(faces[2].poly)-area) > 1e-7 {
			t.Fatal("merge left a gap or overlapping geometry")
		}
		for _, p := range faces[2].poly {
			if p.Y > 200+1e-7 {
				t.Fatal("merge crossed the guide")
			}
		}
		for _, p := range []V{{130, 199.5}, {130, 180}, {180, 180}} {
			if !insideFace(p, faces[2].poly) {
				t.Fatalf("merged face lost part of its inputs at %v", p)
			}
		}
		if insideFace(V{180, 199.5}, faces[2].poly) {
			t.Fatal("union filled the concave notch outside its inputs")
		}
	}
}

func TestFragmentMergePreservesOrdinaryCellsAndProtectedBorders(t *testing.T) {
	a := []V{{100, 199}, {120, 199}, {120, 200}, {100, 200}}
	b := []V{{100, 200}, {140, 200}, {140, 240}, {100, 240}}
	for _, cut := range []bool{false, true} {
		poly := a
		if !cut {
			// Small but compact ordinary cells need no consolidation.
			poly = []V{{100, 194}, {106, 194}, {106, 200}, {100, 200}}
		}
		faces := []guideFragment{makeGuideFragment(poly, faceCenter(poly), cut), makeGuideFragment(b, faceCenter(b), false)}
		g := splineGuide([]V{{90, 200}, {150, 200}}, 1)
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
	top := sectionWindowTop(0)
	noise := NewPerlin(rand.New(rand.NewSource(seed)))
	noise.OffsetY = top
	guides := worldGuides(seed, 0)
	seeds := worldSeeds(seed, top, noise)
	// Measure the actual fragments created by clipping before consolidation.
	before := 0
	for i, site := range seeds {
		poly := voronoiCell(i, seeds)
		faces := [][]V{poly}
		radius := 0.0
		for _, p := range poly {
			radius = math.Max(radius, p.Sub(site).Len())
		}
		for j := range guides {
			if guides[j].distanceBound2(site) > radius*radius {
				continue
			}
			var next [][]V
			for _, face := range faces {
				next = append(next, splitGuideFace(face, &guides[j])...)
			}
			faces = next
		}
		if len(faces) > 1 {
			for _, face := range faces {
				if makeGuideFragment(face, faceCenter(face), true).tiny() {
					before++
				}
			}
		}
	}
	grid := newGuideRockGrid(seeds, guides, func(V) color.NRGBA { return color.NRGBA{A: 255} })
	after, area := 0, 0.0
	for _, face := range grid {
		area += faceArea(face.Polygon)
		if makeGuideFragment(face.Polygon, face.Center, true).tiny() {
			after++
		}
		if !insideFace(face.Center, face.Polygon) || len(faceTriangles(face.Polygon)) != len(face.Polygon)-2 {
			t.Fatal("generated face is not renderable or samples outside its perimeter")
		}
	}
	t.Logf("tiny guide fragments: %d before merging, %d after", before, after)
	if before == 0 || after > before/10 {
		t.Fatalf("too many clipping slivers remain: %d of %d", after, before)
	}
	if math.Abs(area-W*H) > 1e-3 {
		t.Fatalf("merging changed the total terrain area: %v", area)
	}
}

func TestLongThinCellsMergeAtAnyScale(t *testing.T) {
	for _, cut := range []bool{false, true} {
		for _, scale := range []float64{1, 3} {
			// Area and absolute width both exceed the old cutoffs.
			polys := [][]V{
				{{0, 0}, {100, 0}, {100, 8}, {0, 8}},
				{{0, 8}, {100, 8}, {100, 80}, {0, 80}},
			}
			var faces []guideFragment
			for _, poly := range polys {
				for i := range poly {
					// Rotate to ensure the criterion is not axis aligned.
					p := poly[i].Mul(scale)
					poly[i] = V{100 + (p.X-p.Y)/math.Sqrt2, 100 + (p.X+p.Y)/math.Sqrt2}
				}
				faces = append(faces, makeGuideFragment(poly, faceCenter(poly), cut))
			}
			mergeGuideFragments(faces, nil)
			if faces[0].poly != nil || faces[1].tiny() {
				t.Fatalf("long strip survived: cut=%v scale=%v", cut, scale)
			}
			if math.Abs(faces[1].area-8000*scale*scale) > 1e-7 || len(faceTriangles(faces[1].poly)) != len(faces[1].poly)-2 {
				t.Fatal("merge changed coverage or produced an unrenderable face")
			}
		}
	}
}

func TestThinCellChoosesBroaderUnion(t *testing.T) {
	// The lower neighbor shares more border, but the upper neighbor
	// produces a more compact union. Neither neighbor needs merging itself.
	polys := [][]V{
		{{0, 0}, {100, 0}, {100, 8}, {0, 8}},
		{{0, -60}, {280, -60}, {280, 0}, {0, 0}},
		{{10, 8}, {90, 8}, {90, 38}, {10, 38}},
	}
	var faces []guideFragment
	for _, poly := range polys {
		faces = append(faces, makeGuideFragment(poly, faceCenter(poly), true))
	}
	mergeGuideFragments(faces, nil)
	if faces[0].poly != nil || !reflect.DeepEqual(faces[1].poly, polys[1]) || faces[2].area != 3200 {
		t.Fatal("thin cell did not choose the broader union")
	}
}

func TestThinCellCanMergeIntoSmallerNeighbor(t *testing.T) {
	polys := [][]V{
		{{0, 0}, {100, 0}, {100, 8}, {0, 8}},
		{{30, 8}, {70, 8}, {70, 26}, {30, 26}},
	}
	var faces []guideFragment
	for _, poly := range polys {
		faces = append(faces, makeGuideFragment(poly, faceCenter(poly), true))
	}
	mergeGuideFragments(faces, nil)
	if faces[0].poly != nil || faces[1].area != 1520 {
		t.Fatal("thin cell survived despite having a suitable smaller neighbor")
	}
}

func TestThinCellsDoNotMergeIntoThinnerUnion(t *testing.T) {
	// Joining these strips end to end worsens compactness. Accepting this
	// merge lets an already thin face keep growing instead of repairing it.
	polys := [][]V{
		{{0, 0}, {100, 0}, {100, 8}, {0, 8}},
		{{100, 0}, {200, 0}, {200, 8}, {100, 8}},
	}
	var faces []guideFragment
	for _, poly := range polys {
		faces = append(faces, makeGuideFragment(poly, faceCenter(poly), false))
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
	polys := [][]V{
		{{0, 0}, {80, 0}, {0, 24}},
		{{80, 0}, {80, 60}, {0, 60}, {0, 24}},
	}
	var faces []guideFragment
	for _, poly := range polys {
		faces = append(faces, makeGuideFragment(poly, faceCenter(poly), true))
	}
	guide := splineGuide([]V{{0, 0}, {80, 0}}, 1)
	mergeGuideFragments(faces, []Guide{guide})
	if faces[0].poly != nil || faces[1].tiny() || math.Abs(faces[1].area-4800) > 1e-7 {
		t.Fatal("tapered cell survived or merge lost coverage")
	}
}
