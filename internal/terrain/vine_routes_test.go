package terrain

import (
	"math"
	"math/rand"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestStreamedVinesStaySupportedAndKeepForksAttached(t *testing.T) {
	for _, mode := range []Orientation{Vertical, Horizontal} {
		data := NewSectionBuilder(42, nil, mode).Build(0)
		for layer, vines := range [][]Vine{data.Vines, data.ForegroundVines} {
			grid := data.Background
			if layer == 1 {
				grid = data.ForegroundBake.Grid
			}
			graphs := map[VineRoute]*vineEdgeGraph{CellEdgeVineRoute: newVineEdgeGraph(grid), CellCenterVineRoute: newVineCenterGraph(grid)}
			indices := make(map[VineRoute]*geom.BoundsIndex)
			for route, g := range graphs {
				bounds := make([]geom.Bounds, len(g.edges))
				for i, e := range g.edges {
					a, b := g.points[e.a], g.points[e.b]
					bounds[i] = geom.Bounds{Min: geom.V{X: min(a.X, b.X) - .012, Y: min(a.Y, b.Y) - .012}, Max: geom.V{X: max(a.X, b.X) + .012, Y: max(a.Y, b.Y) + .012}}
				}
				indices[route] = geom.NewBoundsIndex(bounds)
			}
			roots, forks := 0, 0
			support := newPathSupport(data.ForegroundBake, data.Guides)
			for i, v := range vines {
				vineSupport := support.forVine(v)
				if layer == 1 {
					if v.topCellLimit < 6 || v.topCellLimit > 9 || len(v.allowedTopCells) > v.topCellLimit {
						t.Fatal("vine exceeded its seeded six-to-nine-cell allowance")
					}
					if v.Parent >= 0 && !reflect.DeepEqual(v.allowedTopCells, vines[v.Parent].allowedTopCells) {
						t.Fatal("fork reset its family's top-cell allowance")
					}
				}
				if v.Parent < 0 {
					roots++
				} else {
					forks++
					if v.Parent >= i || v.Joint >= len(vines[v.Parent].Points) || v.Points[0].P != vines[v.Parent].Points[v.Joint].P {
						t.Fatal("detached fork")
					}
				}
				if v.EdgeAligned || v.Points[len(v.Points)-1].Radius != 0 {
					t.Fatal("route lost its curvature or taper")
				}
				for j := 1; j < len(v.Points); j++ {
					a, b := v.Points[j-1].P, v.Points[j].P
					if math.IsNaN(b.X) || math.IsNaN(b.Y) {
						t.Fatal("nonfinite curve")
					}
					if layer == 0 {
						continue
					}
					radius := max(v.Points[j-1].Radius, v.Points[j].Radius)
					if !vineSupport.fits(a, b, radius) {
						t.Fatalf("unsupported rounded ribbon: %v–%v", a, b)
					}
					if v.Route == FreeVineRoute || v.Route == GuideVineRoute {
						continue
					}
					g, index := graphs[v.Route], indices[v.Route]
					for _, p := range []geom.V{a, b, geom.LerpVector(a, b, .5)} {
						found := false
						index.Query(geom.Bounds{Min: p, Max: p}, func(i int) bool {
							e := g.edges[i]
							found = geom.SegmentDistanceSquared(p, p, g.points[e.a], g.points[e.b]) < .012*.012
							return !found
						})
						if !found {
							t.Fatal("rounded route escaped its facet corridor")
						}
					}
				}
			}
			if roots == 0 || forks == 0 {
				t.Fatalf("mode %v layer %d has too few forks: %d roots, %d forks", mode, layer, roots, forks)
			}
		}
	}
}

func TestVineRouteSelectionIncludesParallelFamiliesAndFreeBranches(t *testing.T) {
	var grid RockGrid
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			grid = append(grid, terrainRect(.1+float64(x)*.05, .1+float64(y)*.05, .05, .05))
		}
	}
	support := newPathSupport(NewRockTopology(grid), nil)
	source := []Vine{freePath(geom.V{X: .3, Y: .3}, geom.V{Y: 1}, .5, .0013, 0, -1, 0)}
	var edgeOnly, centerOnly, paired, free int
	for seed := int64(1); seed <= 20; seed++ {
		vines := alignPathVines(grid, source, &support, seed)
		if !reflect.DeepEqual(vines, alignPathVines(grid, source, &support, seed)) {
			t.Fatal("route selection is not deterministic")
		}
		roots := make(map[VineRoute]geom.V)
		for i, v := range vines {
			if v.Parent < 0 {
				roots[v.Route] = v.Points[0].P
				if v.Route == CellCenterVineRoute {
					found := false
					for _, cell := range grid {
						found = found || v.Points[0].P == cell.Center
					}
					if !found {
						t.Fatal("center route root missed the cell centers")
					}
				}
			} else if v.Route == FreeVineRoute {
				free++
				if v.Points[0].P != vines[v.Parent].Points[v.Joint].P || v.Depth != vines[v.Parent].Depth+1 {
					t.Fatal("free branch lost its parent joint")
				}
				if v.Points[0].Radius > vines[v.Parent].Points[v.Joint].Radius {
					t.Fatal("free branch is thicker than its parent")
				}
			}
			for j := 1; j < len(v.Points); j++ {
				if !support.fits(v.Points[j-1].P, v.Points[j].P, max(v.Points[j-1].Radius, v.Points[j].Radius)) {
					t.Fatalf("seed %d vine %d left its supporting rock", seed, i)
				}
			}
		}
		edgeRoot, edge := roots[CellEdgeVineRoute]
		centerRoot, center := roots[CellCenterVineRoute]
		switch {
		case edge && center:
			paired++
			if edgeRoot.Sub(centerRoot).Len() > .08 {
				t.Fatal("paired routes did not grow next to one another")
			}
		case edge:
			edgeOnly++
		case center:
			centerOnly++
		}
	}
	if edgeOnly == 0 || centerOnly == 0 || paired == 0 || free < 20 {
		t.Fatalf("missing route variety or small branches: edge=%d center=%d paired=%d free=%d", edgeOnly, centerOnly, paired, free)
	}

	// A free twig can leave a straight stem at an arbitrary sample, regardless
	// of cell geometry, with a tapered tip and a substantial sideways span.
	stem := freePath(geom.V{X: .4, Y: .3}, geom.V{Y: 1}, .4, .0025, 0, -1, 0)
	branches := addFreeVineBranches([]Vine{stem}, nil, rand.New(rand.NewSource(42)), nil)
	if len(branches) < 3 {
		t.Fatal("straight stem generated too few free branches")
	}
	for _, v := range branches[1:] {
		tangent := stem.Points[v.Joint+1].P.Sub(stem.Points[v.Joint-1].P).Norm()
		sideways := 0.0
		for _, p := range v.Points {
			sideways = max(sideways, math.Abs(p.P.Sub(v.Points[0].P).Dot(tangent.Perp())))
		}
		if v.Route != FreeVineRoute || v.Points[len(v.Points)-1].Radius != 0 || sideways < .008 {
			t.Fatal("free offshoot lost its taper or sideways growth")
		}
	}
}

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

func TestVineTopAllowanceCountsDistinctCellsAndStopsAtTheLimit(t *testing.T) {
	for _, mode := range []Orientation{Vertical, Horizontal} {
		var grid RockGrid
		up := InternalPoint(mode, geom.V{Y: -.6})
		for i := 0; i < 9; i++ {
			c := terrainRect(.1+float64(i)*.08, .2, .08, .4)
			MapPoints(c.Polygon, mode.internalLocal)
			c.Center, c.orientation = mode.internalLocal(c.Center), mode
			if i%2 == 1 {
				c.Normal = geom.V3{X: up.X, Y: up.Y, Z: .8}
			}
			grid = append(grid, c)
		}
		support := newPathSupport(NewRockTopology(grid), nil)
		for _, limit := range []int{0, 2, 3} {
			g := newVineCenterGraph(grid)
			r := newVineRoutesForGraph(g, &support, CellCenterVineRoute)
			v, joints := r.trace(g.points[0], 0, g.edges[0].b, nil, InternalPoint(mode, geom.V{X: 1}), .8, .0013, rand.New(rand.NewSource(42)), vineTopAllowance{limit: limit})
			if limit == 0 {
				if len(v.Points) != 0 {
					t.Fatal("zero allowance crossed an upward-facing cell")
				}
				continue
			}
			if len(v.allowedTopCells) != limit || v.topCellLimit != limit || v.Points[len(v.Points)-1].P != g.points[limit*2] {
				t.Fatalf("mode %v limit %d: route failed to cross its allowed cells or continued into the next", mode, limit)
			}
			// Both halves of each center-to-midpoint crossing count the same
			// cell once, even after resampling and smoothing the route.
			var pins []int
			for _, joint := range joints {
				if joint.node >= g.centerCount {
					pins = append(pins, joint.point)
				}
			}
			v = smoothVineRoutes([]Vine{v}, &support, [][]int{pins})[0]
			growth := support.forVine(v)
			touched := make(map[int]bool)
			for i := 1; i < len(v.Points); i++ {
				a, b := v.Points[i-1], v.Points[i]
				if !growth.fits(a.P, b.P, max(a.Radius, b.Radius)) {
					t.Fatal("smoothing escaped the permitted cells or rock support")
				}
				support.visitTopCells(a.P, b.P, max(a.Radius, b.Radius), func(cell int) bool {
					touched[cell] = true
					return true
				})
			}
			if len(touched) != limit {
				t.Fatalf("limit %d was counted per sample instead of per cell: %d cells", limit, len(touched))
			}
		}
	}
}

func TestVineTrunksStartOnAndCrossBrightRidges(t *testing.T) {
	for _, mode := range []Orientation{Vertical, Horizontal} {
		var grid RockGrid
		for i := 0; i < 13; i++ {
			cell := terrainRect(.15+float64(i)*.05, .2, .05, .4)
			MapPoints(cell.Polygon, mode.internalLocal)
			cell.Center, cell.orientation = mode.internalLocal(cell.Center), mode
			grid = append(grid, cell)
		}
		topology := NewRockTopology(grid)
		topology.Appearance = append(RockGrid(nil), grid...)
		up := InternalPoint(mode, geom.V{Y: -.6})
		for i := range topology.Appearance {
			topology.Appearance[i].Normal = geom.V3{X: up.X, Y: up.Y, Z: .8}
			topology.Appearance[i].Color.R = 180
		}
		support := newPathSupport(topology, nil)
		source := Vine{Parent: -1, Foreground: true}
		for _, cell := range grid {
			source.Points = append(source.Points, VinePoint{P: cell.Center, Radius: .0013})
		}
		for _, seed := range []int64{1, 42, 123} {
			grow := func() Vine {
				routes := newVineRoutesForGraph(newVineCenterGraph(grid), &support, CellCenterVineRoute)
				v, _ := routes.trunk(source, rand.New(rand.NewSource(seed)), seed)
				return v
			}
			v := grow()
			if !surfaceVineLongEnough(v) || len(v.allowedTopCells) < 6 || len(v.allowedTopCells) > v.topCellLimit {
				t.Fatalf("mode %v seed %d: vine failed to traverse a bright ridge: %d cells", mode, seed, len(v.allowedTopCells))
			}
			if v.Points[0].P != source.Points[0].P {
				t.Fatal("vine could not start inside a bright cell")
			}
			if !reflect.DeepEqual(v, grow()) {
				t.Fatal("bright ridge route changed on reload")
			}
			growth := support.forVine(v)
			for i := 1; i < len(v.Points); i++ {
				a, b := v.Points[i-1], v.Points[i]
				if !growth.fits(a.P, b.P, max(a.Radius, b.Radius)) {
					t.Fatal("bright ridge crossing left its rock support or allowed cells")
				}
			}
		}
	}
}

func TestVineForkCannotResetItsParentsTopCellAllowance(t *testing.T) {
	grid := RockGrid{terrainRect(.1, .2, .2, .4), terrainRect(.3, .2, .2, .4), terrainRect(.5, .2, .2, .4)}
	for i := range grid {
		grid[i].Normal = geom.V3{Y: -.6, Z: .8}
	}
	support := newPathSupport(NewRockTopology(grid), nil)
	g := newVineCenterGraph(grid)
	r := newVineRoutesForGraph(g, &support, CellCenterVineRoute)
	allowance := vineTopAllowance{limit: 2, cells: []int{0, 1}}
	v, _ := r.trace(g.points[1], 2, g.edges[2].b, nil, geom.V{X: 1}, .4, .001, rand.New(rand.NewSource(42)), allowance)
	if len(v.Points) != 0 {
		t.Fatal("fork with a spent allowance crossed a third upward-facing cell")
	}
	// Existing permitted cells remain usable without spending another slot.
	v, _ = r.trace(g.points[1], 1, g.edges[1].b, nil, geom.V{X: -1}, .4, .001, rand.New(rand.NewSource(42)), allowance)
	if len(v.Points) == 0 || len(v.allowedTopCells) != 2 {
		t.Fatal("fork could not reuse its parent's permitted cells")
	}
}
