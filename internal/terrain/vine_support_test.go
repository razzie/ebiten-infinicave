package terrain

import (
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

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
