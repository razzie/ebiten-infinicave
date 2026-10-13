package terrain

import (
	"math/rand"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestVineCentersConnectOnlyAcrossSharedEdges(t *testing.T) {
	grid := RockGrid{
		terrainRect(.2, .2, .05, .05),
		terrainRect(.25, .2, .05, .05),
		terrainRect(.3, .25, .05, .05),  // Corner contact only.
		terrainRect(.2, .251, .05, .05), // A narrow gap.
	}
	// Clipping can leave an original site outside its remaining polygon.
	grid[1].Center = geom.V{X: .9, Y: .9}
	g := newVineCenterGraph(grid)
	if len(g.edges) != 2 || g.edges[0] != (vineEdge{0, 4}) || g.edges[1] != (vineEdge{1, 4}) {
		t.Fatalf("centers connected across a corner or gap: %+v", g.edges)
	}
	for i, p := range g.points[:g.centerCount] {
		if !geom.InsidePolygon(p, grid[i].Polygon) {
			t.Fatal("center route uses a site outside its cell")
		}
	}
	if g.points[4].Sub(geom.V{X: .25, Y: .225}).Len() > 1e-12 {
		t.Fatal("center route missed the shared edge midpoint")
	}
}

func TestVineCentersCrossEdgeMidpointsAfterSmoothing(t *testing.T) {
	grid := RockGrid{terrainRect(.2, .2, .16, .16), terrainRect(.36, .2, .16, .16)}
	grid[0].Center = geom.V{X: .235, Y: .32}
	grid[1].Center = geom.V{X: .495, Y: .225}
	g := newVineCenterGraph(grid)
	r := newVineRoutesForGraph(g, nil, CellCenterVineRoute)
	v, joints := r.trace(g.points[0], 0, 2, nil, geom.V{X: 1}, .35, .0013, rand.New(rand.NewSource(42)))
	if len(joints) != 2 {
		t.Fatal("vine did not enter its neighboring cell")
	}
	vines := smoothVineRoutes([]Vine{v}, nil, [][]int{{joints[0].point}})
	midpoint := geom.V{X: .36, Y: .28}
	found := false
	for _, p := range vines[0].Points {
		found = found || p.P.Sub(midpoint).Len() < 1e-12
	}
	if !found {
		t.Fatal("curve smoothing moved the crossing away from the edge midpoint")
	}
}
