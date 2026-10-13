package terrain

import (
	"math/rand"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestVineRoutesFindLongAlternativesToDeadEnds(t *testing.T) {
	g := &vineEdgeGraph{
		points: []geom.V{{X: .2, Y: .5}, {X: .3, Y: .5}, {X: .34, Y: .5}, {X: .3, Y: .6}, {X: .4, Y: .6}, {X: .5, Y: .6}},
		edges:  []vineEdge{{0, 1}, {1, 2}, {1, 3}, {3, 4}, {4, 5}},
		links:  [][]int{{0}, {0, 1, 2}, {1}, {2, 3}, {3, 4}, {4}},
	}
	r := newVineRoutesForGraph(g, nil, CellEdgeVineRoute)
	guide := []VinePoint{{P: g.points[0]}, {P: geom.V{X: .6, Y: .5}}}
	v, joints := r.trace(g.points[0], 0, 1, guide, geom.V{X: 1}, .35, .0013, rand.New(rand.NewSource(42)))
	length, _ := v.extent()
	if length < .35 || len(joints) != 4 {
		t.Fatal("vine stopped at the short forward spur instead of taking the longer side route")
	}
	if r.used[1] || !r.used[2] || !r.used[4] {
		t.Fatal("route search reserved a discarded dead end")
	}
	if v.Points[len(v.Points)-1].Radius != 0 {
		t.Fatal("longer route lost its tapered tip")
	}
}
