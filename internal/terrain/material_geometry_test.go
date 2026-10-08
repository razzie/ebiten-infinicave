package terrain

import (
	"image/color"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestMaterialBindingKeepsShapeVersionButDistinguishesRuntimeCuts(t *testing.T) {
	cell := RockCell{Center: geom.V{X: .5, Y: .5}, Polygon: []geom.V{{X: .1, Y: .1}, {X: .9, Y: .1}, {X: .9, Y: .9}, {X: .1, Y: .9}}, Color: color.NRGBA{A: 255}, Normal: geom.V3{Z: 1}, Raised: true}
	early := PrepareTerrainGeometry(SectionData{Foreground: RockGrid{cell}}, 0)
	topology := *early.Topology
	topology.Grid = append(RockGrid(nil), topology.Grid...)
	topology.Grid[0].Color.R = 100
	material := BindTerrainMaterial(early, SectionData{ForegroundTopology: &topology})
	if !SameTerrain(early, material) || early.Grid[0].Color.R != 0 || material.Grid[0].Color.R != 100 {
		t.Fatal("material changed occupancy or mutated the early snapshot")
	}
	left, _ := CutFromHole(Hole{Shape: HoleCircle, Center: geom.V{X: .3, Y: -.5}, Radius: .05})
	right, _ := CutFromHole(Hole{Shape: HoleCircle, Center: geom.V{X: .7, Y: -.5}, Radius: .05})
	a, _, _ := left.Geometry(0, early, 0)
	b, _, _ := left.Geometry(0, material, 0)
	c, _, _ := right.Geometry(0, material, 0)
	if !SameTerrain(a, b) || SameTerrain(a, c) || SameTerrain(a, early) {
		t.Fatal("terrain reuse ignored edits or confused material with shape changes")
	}
}
