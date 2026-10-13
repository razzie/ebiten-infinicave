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
					if v.topCellLimit < 2 || v.topCellLimit > 3 || len(v.allowedTopCells) > v.topCellLimit {
						t.Fatal("vine exceeded its seeded two-or-three-cell allowance")
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
	branches := addFreeVineBranches([]Vine{stem}, nil, rand.New(rand.NewSource(42)))
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
