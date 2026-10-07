package render

import (
	"image/color"
	"math"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestHorizontalNormalDiagnostic(t *testing.T) {
	up := terrain.InternalPoint(terrain.Horizontal, geom.V{X: 0, Y: -1})
	// The normal diagnostic must report world axes too.
	cell := terrain.WithRockOrientation(terrain.RockCell{Normal: geom.V3{X: up.X, Y: up.Y, Z: 0}}, terrain.Horizontal)
	if clr := rockViewColor(cell, ViewNormals); clr.G != 0 || clr.R != 127 {
		t.Fatalf("horizontal normal diagnostic: %+v", clr)
	}
}

func TestExposedRockEdgesCancelPartialNeighbors(t *testing.T) {
	grid := terrain.RockGrid{
		{Center: geom.V{X: 0.11, Y: 0.11}, Polygon: []geom.V{{X: 0.1, Y: 0.1}, {X: 0.12, Y: 0.1}, {X: 0.12, Y: 0.12}, {X: 0.1, Y: 0.12}}},
		{Center: geom.V{X: 0.125, Y: 0.105}, Polygon: []geom.V{{X: 0.12, Y: 0.1}, {X: 0.13, Y: 0.1}, {X: 0.13, Y: 0.11}, {X: 0.12, Y: 0.11}}},
	}
	edges := terrain.ExposedRockEdges(grid)
	length := 0.0
	for _, e := range edges {
		length += e.B.Sub(e.A).Len()
		if e.A.X == .120 && e.B.X == .120 && (e.A.Y+e.B.Y)*.5 < .110 {
			t.Fatal("shared partial edge incorrectly exposes a side wall")
		}
	}
	if math.Abs(length-.100) > 1e-9 {
		t.Fatalf("wrong exposed perimeter: %v", length)
	}
	for i := range grid {
		grid[i].Raised, grid[i].Z, grid[i].Normal = true, .060, geom.V3{Z: 1}
		grid[i].Shadow, grid[i].Ambient = 1, 1
		grid[i].Color = color.NRGBA{R: 100, G: 100, B: 100, A: 255}
	}
	vertices, indices := appendRockWalls(nil, nil, grid, edges, ViewClay)
	if len(indices) == 0 {
		t.Fatal("raised silhouette has no side geometry")
	}
	for _, v := range vertices {
		if math.IsNaN(float64(v.DstX)) || v.ColorR != v.ColorG || v.ColorG != v.ColorB {
			t.Fatal("invalid or non-neutral wall in clay view")
		}
	}
}
