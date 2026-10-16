package terrain

import (
	"math"
	"math/rand"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestBackgroundVinesIncludeHorizontalRunners(t *testing.T) {
	for _, mode := range []Orientation{Vertical, Horizontal} {
		for _, seed := range []int64{7, 42, 123} {
			for _, id := range []int64{0, 1, 2} {
				vines := backgroundPathVines(seed, id, mode)
				if !reflect.DeepEqual(vines, backgroundPathVines(seed, id, mode)) {
					t.Fatalf("mode %v seed %d section %d: background vines changed on reload", mode, seed, id)
				}
				roots, horizontal := 0, 0
				for i, v := range vines {
					if v.Foreground {
						t.Fatal("background vine became foreground growth")
					}
					lo, hi := v.Points[0].P, v.Points[0].P
					for _, p := range v.Points {
						if p.P.X < .02 || p.P.X > .98 || p.P.Y < GenerationMinY+.02 || p.P.Y > generationMaxY-.02 {
							t.Fatal("background vine escaped the padded section window")
						}
						lo.X, lo.Y = min(lo.X, p.P.X), min(lo.Y, p.P.Y)
						hi.X, hi.Y = max(hi.X, p.P.X), max(hi.Y, p.P.Y)
					}
					if v.Parent >= 0 {
						if v.Parent >= i || v.Points[0].P != vines[v.Parent].Points[v.Joint].P {
							t.Fatal("background fork lost its parent attachment")
						}
						continue
					}
					roots++
					span := WorldPoint(mode, hi.Sub(lo))
					if math.Abs(span.X) > math.Abs(span.Y) {
						horizontal++
						if math.Abs(span.X) < .35 {
							t.Fatal("horizontal background runner is too short")
						}
					}
				}
				wantRoots, wantHorizontal := 7, 2
				if roots != wantRoots || horizontal != wantHorizontal {
					t.Fatalf("mode %v seed %d section %d: got %d roots (%d horizontal), want %d (%d horizontal)", mode, seed, id, roots, horizontal, wantRoots, wantHorizontal)
				}
			}
		}
	}
}

func TestForegroundVineLengths(t *testing.T) {
	for _, mode := range []Orientation{Vertical, Horizontal} {
		roots, length, longest := 0, 0.0, 0.0
		for _, seed := range []int64{7, 42} {
			for _, id := range []int64{0, -1} {
				data := NewSectionBuilder(seed, nil, mode).Build(id)
				for _, v := range data.ForegroundVines {
					if v.Parent >= 0 {
						continue
					}
					if !surfaceVineLongEnough(v) {
						t.Fatalf("mode %v seed %d section %d retained a short isolated vine", mode, seed, id)
					}
					n, _ := v.extent()
					roots++
					length += n
					longest = max(longest, n)
				}
			}
		}
		mean := length / float64(roots)
		t.Logf("mode %v: %d roots, mean length %.4f, longest %.4f", mode, roots, mean, longest)
		if roots < 8 || mean < .20 || longest < .35 {
			t.Fatalf("mode %v: top-cell avoidance shortened the main vines too much", mode)
		}
	}
}

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

func TestVinesRejectUnallocatedRenderedTopFacingCells(t *testing.T) {
	for _, mode := range []Orientation{Vertical, Horizontal} {
		grid := RockGrid{terrainRect(.1, .2, .8, .1), terrainRect(.1, .3, .8, .4)}
		for i := range grid {
			MapPoints(grid[i].Polygon, mode.internalLocal)
			grid[i].Center = mode.internalLocal(grid[i].Center)
			grid[i].orientation = mode
			grid[i].Normal = geom.V3{Z: 1}
		}
		topology := NewRockTopology(grid)
		topology.Appearance = append(RockGrid(nil), topology.Grid...)
		up := InternalPoint(mode, geom.V{Y: -.6})
		topology.Appearance[0].Normal = geom.V3{X: up.X, Y: up.Y, Z: .8}
		support := newPathSupport(topology, nil)
		for _, route := range []VineRoute{FreeVineRoute, CellEdgeVineRoute, CellCenterVineRoute, GuideVineRoute} {
			s := support.forRoute(route)
			p := mode.internalLocal(geom.V{X: .5, Y: .25})
			if s.fits(p, p, .001) {
				t.Fatalf("mode %v route %v: vine grew on a rendered top-facing cell", mode, route)
			}
			p = mode.internalLocal(geom.V{X: .5, Y: .35})
			if !s.fits(p, p, .001) {
				t.Fatalf("mode %v route %v: vine cannot grow below the top-facing cell", mode, route)
			}
			// An eligible centerline cannot let its outer ribbon cover the top.
			p = mode.internalLocal(geom.V{X: .5, Y: .303})
			if s.fits(p, p, .003) {
				t.Fatal("vine width crossed onto a top-facing cell")
			}
		}
	}
}

func TestSparseVineSegmentsCannotCrossUnallocatedTopFacingCells(t *testing.T) {
	grid := RockGrid{terrainRect(.1, .2, .3, .6), terrainRect(.4, .2, .1, .6), terrainRect(.5, .2, .4, .6)}
	grid[1].Normal = geom.V3{Y: -.6, Z: .8}
	support := newPathSupport(NewRockTopology(grid), nil)
	a, b := geom.V{X: .3, Y: .5}, geom.V{X: .6, Y: .5}
	if !support.fits(a, a, .001) || !support.fits(b, b, .001) {
		t.Fatal("fixture endpoints do not support vines")
	}
	if support.fits(a, b, .001) {
		t.Fatal("sparse ribbon crossed a top-facing cell between its samples")
	}
}
