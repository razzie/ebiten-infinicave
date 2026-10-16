package terrain

import (
	"math"
	"math/rand"
	"reflect"
	"slices"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestForcedGuideSidesFaceUpWithoutFlippingAlongBends(t *testing.T) {
	// The hook contains both leftward and rightward runs and vertical edges.
	points := []geom.V{{X: .2, Y: .3}, {X: .7, Y: .3}, {X: .7, Y: .6}, {X: .3, Y: .6}, {X: .3, Y: .8}}
	for _, mode := range []Orientation{Vertical, Horizontal} {
		for _, sign := range []float64{1, -1} {
			for _, reverse := range []bool{false, true} {
				knots := slices.Clone(points)
				if reverse {
					slices.Reverse(knots)
				}
				MapPoints(knots, mode.internalLocal)
				g := SplineGuide(knots, sign)
				g.faceUp(mode)
				g.prepareProjection()
				mean := geom.V{}
				var previous geom.V
				for i := 0; i < len(g.Pts)-1; i++ {
					mean = mean.Add(g.Pts[i+1].Sub(g.Pts[i]).Perp().Mul(g.BrightSign))
					s := (g.S[i] + g.S[i+1]) / 2
					q, _, n := g.frameAt(s)
					if i > 0 && n.Dot(previous) < .9 {
						t.Fatalf("%v sign=%g reverse=%v: side flips along bend: %v / %v", mode, sign, reverse, previous, n)
					}
					previous = n
					pr := g.project(q.Add(n.Mul(.01)))
					_, _, projectedNormal := g.frameAt(pr.S)
					if pr.N.Sub(projectedNormal).Len() > 1e-12 || pr.Signed <= 0 {
						t.Fatalf("%v: projection and sampled frame disagree: %+v / %v", mode, pr, projectedNormal)
					}
				}
				if WorldPoint(mode, mean).Y <= 0 {
					t.Fatalf("%v sign=%g reverse=%v: whole guide exposes its underside", mode, sign, reverse)
				}
				// Public horizontal sections preserve the chosen side on rotation.
				sample := g.S[len(g.S)-1] / 3
				_, _, before := g.frameAt(sample)
				section := OrientedSection(mode, Section{Guides: []Guide{g}})
				out := section.Guides[0]
				_, _, after := out.frameAt(sample)
				if after.Sub(WorldPoint(mode, before)).Len() > 1e-12 {
					t.Fatal("public guide lost its chosen side")
				}
			}
		}
	}
}

func TestGuideSidesPreservePointOrderAndBrightSignByDefault(t *testing.T) {
	for _, sign := range []float64{1, -1} {
		for _, reverse := range []bool{false, true} {
			points := []geom.V{{X: .2, Y: .4}, {X: .8, Y: .4}}
			if reverse {
				slices.Reverse(points)
			}
			g := SplineGuide(points, sign)
			want := sign
			if reverse {
				want = -want
			}
			pr := g.project(geom.V{X: .5, Y: .45})
			_, _, n := g.frameAt(g.S[len(g.S)-1] / 2)
			if n != (geom.V{Y: want}) || pr.N != n || math.Abs(pr.Signed-.05*want) > 1e-12 {
				t.Fatalf("default side changed: sign=%g reverse=%v: %+v / %v", sign, reverse, pr, n)
			}
		}
	}
}

func TestForcedGuideSidesIndependentOfPointOrder(t *testing.T) {
	for _, points := range [][]geom.V{
		{{X: .2, Y: .4}, {X: .8, Y: .4}},
		{{X: .5, Y: .2}, {X: .5, Y: .8}},
		{{X: .2, Y: .25}, {X: .7, Y: .25}, {X: .7, Y: .75}, {X: .2, Y: .75}, {X: .2, Y: .25}},
	} {
		for _, mode := range []Orientation{Vertical, Horizontal} {
			knots := slices.Clone(points)
			MapPoints(knots, mode.internalLocal)
			forward := SplineGuide(knots, 1)
			forward.faceUp(mode)
			slices.Reverse(knots)
			for _, sign := range []float64{1, -1} {
				reverse := SplineGuide(knots, sign)
				reverse.faceUp(mode)
				for _, point := range []geom.V{{X: .31, Y: .411}, {X: .487, Y: .573}, {X: .812, Y: .622}} {
					p := mode.internalLocal(point)
					a, b := forward.project(p), reverse.project(p)
					if a.N.Sub(b.N).Len() > 1e-10 || math.Abs(a.Signed-b.Signed) > 1e-10 {
						t.Fatalf("%v sign=%g: reversing guide points changed the rock side at %v: %+v / %+v", mode, sign, point, a, b)
					}
				}
			}
		}
	}
}

func TestPreparedGuideProjectionMatchesFullSearch(t *testing.T) {
	rng := rand.New(rand.NewSource(43))
	for _, guide := range []Guide{
		SplineGuide([]geom.V{{X: .1, Y: .6}, {X: .6, Y: .2}, {X: .9, Y: .8}, {X: .4, Y: .9}, {X: .1, Y: .6}}, 1),
		{Pts: []geom.V{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 0}, {X: 0, Y: 0}}, S: []float64{0, 1, 1, 2}, BrightSign: 1},
	} {
		prepared := guide
		prepared.prepareProjection()
		points := append([]geom.V(nil), guide.Pts...)
		for range 1000 {
			points = append(points, geom.V{X: rng.Float64()*3 - 1, Y: rng.Float64()*3 - 1})
		}
		for _, p := range points {
			if got, want := prepared.project(p), guide.project(p); got != want {
				t.Fatalf("point %v: projection changed: %+v / %+v", p, got, want)
			}
		}
		shifted := shiftedGuide(prepared, 3)
		if shifted.projection != nil {
			t.Fatal("translated guide retained stale projection cache")
		}
	}
}

func TestPreparedGuideProjectionTailsAndTies(t *testing.T) {
	for count := 2; count <= 27; count++ {
		g := Guide{BrightSign: 1}
		for i := 0; i < count; i++ {
			g.Pts = append(g.Pts, geom.V{X: float64(i % 3), Y: float64(i % 2)})
			g.S = append(g.S, float64(i))
		}
		// Repeat endpoints, including a degenerate segment inside a full block.
		if count > 8 {
			g.Pts[7] = g.Pts[6]
		}
		prepared := g
		prepared.prepareProjection()
		for _, p := range append(g.Pts, geom.V{X: .5, Y: .5}, geom.V{X: 1, Y: .5}) {
			if got, want := prepared.project(p), g.project(p); got != want {
				t.Fatalf("count %d point %v: %+v != %+v", count, p, got, want)
			}
		}
	}
}

func TestRidgedGuideHasBroadDeterministicFacets(t *testing.T) {
	original := SplineGuide([]geom.V{{X: 0.1, Y: 0.2}, {X: 0.5, Y: 0.2}}, 1)
	g := RidgedGuide(original, 42)
	if !reflect.DeepEqual(g, RidgedGuide(original, 42)) {
		t.Fatal("ridge changes when regenerated")
	}
	if len(g.Pts) >= len(original.Pts)/2 || len(g.Pts) < 15 {
		t.Fatalf("expected broad facets instead of dense curve samples, got %d", len(g.Pts))
	}
	if g.Pts[0] != original.Pts[0] || g.Pts[len(g.Pts)-1] != original.Pts[len(original.Pts)-1] {
		t.Fatal("roughness moved a guide endpoint")
	}
	minY, maxY := .200, .200
	for _, p := range g.Pts {
		minY, maxY = math.Min(minY, p.Y), math.Max(maxY, p.Y)
		if math.Abs(p.Y-.200) > .0025 {
			t.Fatal("ridge no longer follows the original guide")
		}
	}
	if maxY-minY < .003 {
		t.Fatal("guide border is still smooth")
	}
	// Translating a regeneration window cannot change its geometry.
	original.translateY(-1)
	shifted := RidgedGuide(original, 42)
	for i, p := range g.Pts {
		if p.Sub(shifted.Pts[i].Add(geom.V{X: 0, Y: 1})).Len() > 1e-9 {
			t.Fatal("roughness depends on section position")
		}
	}
}
