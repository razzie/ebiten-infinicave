package terrain

import (
	"math/rand"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestShortVineRetriesFromTheOtherEnd(t *testing.T) {
	g := &vineEdgeGraph{
		points: []geom.V{{X: .2, Y: .2}, {X: .26, Y: .2}, {X: .29, Y: .2}, {X: .4, Y: .6}, {X: .55, Y: .6}, {X: .75, Y: .6}},
		edges:  []vineEdge{{0, 1}, {1, 2}, {3, 4}, {4, 5}},
		links:  [][]int{{0}, {0, 1}, {1}, {2}, {2, 3}, {3}},
	}
	support := newPathSupport(NewRockTopology(RockGrid{terrainRect(.1, .1, .8, .8)}), nil)
	r := newVineRoutesForGraph(g, &support, CellEdgeVineRoute)
	source := Vine{Parent: -1, Points: []VinePoint{{P: g.points[0]}, {P: geom.V{X: .3, Y: .3}}, {P: geom.V{X: .5, Y: .45}}, {P: g.points[5]}}}
	v, _ := r.trunk(source, rand.New(rand.NewSource(42)), 42)
	if len(v.Points) == 0 || !surfaceVineLongEnough(v) || v.Points[0].P != g.points[5] {
		t.Fatal("short root was discarded without trying the longer route from the other end")
	}
	if r.used[0] || r.used[1] || !r.used[2] || !r.used[3] {
		t.Fatal("retry reserved a discarded stub or failed to reserve its longer replacement")
	}
}

func TestShortVineIsOmittedWhenRetriesCannotExtendIt(t *testing.T) {
	g := &vineEdgeGraph{
		points: []geom.V{{X: .2, Y: .2}, {X: .26, Y: .2}, {X: .29, Y: .2}},
		edges:  []vineEdge{{0, 1}, {1, 2}},
		links:  [][]int{{0}, {0, 1}, {1}},
	}
	support := newPathSupport(NewRockTopology(RockGrid{terrainRect(.1, .1, .8, .8)}), nil)
	r := newVineRoutesForGraph(g, &support, CellEdgeVineRoute)
	source := Vine{Parent: -1, Points: []VinePoint{{P: g.points[0]}, {P: g.points[1]}, {P: g.points[2]}}}
	v, joints := r.trunk(source, rand.New(rand.NewSource(42)), 42)
	if len(v.Points) != 0 || len(joints) != 0 || r.used[0] || r.used[1] {
		t.Fatal("an unextendable isolated stub survived or consumed route space")
	}
}

func TestRemovingShortVineFamiliesKeepsAttachedTwigsAndRemapsParents(t *testing.T) {
	short := Vine{Parent: -1, Foreground: true}
	// A tiny curled shape can accumulate plenty of length without useful span.
	for i := 0; i < 10; i++ {
		short.Points = append(short.Points, VinePoint{P: geom.V{X: .6 + float64(i%2)*.03, Y: .6}, Radius: .001})
	}
	long := Vine{Parent: -1, Foreground: true, Points: []VinePoint{{P: geom.V{X: .1, Y: .2}}, {P: geom.V{X: .5, Y: .2}, Radius: .001}, {P: geom.V{X: .6, Y: .2}}}}
	branch := func(parent, joint, depth int, root geom.V) Vine {
		return Vine{Parent: parent, Joint: joint, Depth: depth, Foreground: true, Points: []VinePoint{{P: root, Radius: .0005}, {P: root.Add(geom.V{X: .02, Y: .02})}}}
	}
	source := []Vine{short, long,
		branch(0, 1, 1, short.Points[1].P), branch(1, 1, 1, long.Points[1].P),
		branch(2, 0, 2, short.Points[1].P), branch(3, 0, 2, long.Points[1].P),
		{Parent: -1, Points: []VinePoint{{P: geom.V{X: .8, Y: .8}}, {P: geom.V{X: .82, Y: .82}}}},
	}
	kept := removeShortSurfaceVines(source)
	if len(kept) != 4 || kept[0].Points[0] != long.Points[0] || kept[1].Parent != 0 || kept[2].Parent != 1 || kept[3].Foreground {
		t.Fatal("short-family cleanup damaged surviving branches or background growth")
	}
	for _, v := range kept[:3] {
		if v.Parent >= 0 && v.Points[0].P != kept[v.Parent].Points[v.Joint].P {
			t.Fatal("cleanup detached a surviving twig")
		}
	}
	if source[3].Parent != 1 || source[5].Parent != 3 {
		t.Fatal("cleanup modified the original family attachments")
	}
}
