package terrain

import (
	"math"
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
