package terrain

import (
	"math/rand"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

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
