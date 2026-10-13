package terrain

import (
	"math/rand"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestContainsAnchorMatchesExactCarvePolygon(t *testing.T) {
	for _, hole := range []Hole{
		{Shape: HoleCircle, Center: geom.V{X: .3, Y: -.8}, Radius: .1},
		{Shape: HoleSegment, Start: geom.V{X: .1, Y: -.2}, End: geom.V{X: .7, Y: -.8}, Width: .06},
	} {
		cut, err := CutFromHole(hole)
		if err != nil {
			t.Fatal(err)
		}
		rng := rand.New(rand.NewSource(42))
		for i := 0; i < 5000; i++ {
			p := geom.V{X: rng.Float64(), Y: -rng.Float64()}
			if got, want := cut.ContainsAnchor(p), geom.InsidePolygon(p, cut.Poly); got != want {
				t.Fatalf("containment mismatch at %+v: got %v, want %v", p, got, want)
			}
		}
		for i, p := range cut.Poly {
			for _, boundary := range []geom.V{p, geom.LerpVector(p, cut.Poly[(i+1)%len(cut.Poly)], .5)} {
				if cut.ContainsAnchor(boundary) {
					t.Fatal("boundary-only contact deleted an anchor")
				}
			}
		}
	}
}
