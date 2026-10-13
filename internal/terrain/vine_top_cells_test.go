package terrain

import (
	"math/rand"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

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
