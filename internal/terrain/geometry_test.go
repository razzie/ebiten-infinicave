package terrain

import (
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestBoundaryCarvingPreservesMaterialAndExactOccupancy(t *testing.T) {
	fixtures := []SectionData{
		BuildSection(42, 0),
		{Foreground: RockGrid{
			terrainRock([]geom.V{{X: .1, Y: -.2}, {X: .5, Y: -.2}, {X: .5, Y: .3}, {X: .3, Y: .3}, {X: .3, Y: .8}, {X: .1, Y: .8}}),
			terrainRect(.3, .3, .2, .5), terrainRect(.5, -.2, .2, 1),
			terrainRect(0, .8, 1, 1.2),
		}},
	}
	holes := []Hole{
		{Shape: HoleCircle, Center: geom.V{X: .4, Y: -.95}, Radius: .12},
		{Shape: HoleSegment, Start: geom.V{X: .2, Y: -.85}, End: geom.V{X: .7, Y: -1.15}, Width: .035},
		{Shape: HoleCircle, Center: geom.V{X: .42, Y: -1.08}, Radius: .06},
		{Shape: HoleSegment, Start: geom.V{X: .1, Y: -.5}, End: geom.V{X: .9, Y: -.5}, Width: .05},
		{Shape: HoleCircle, Center: geom.V{X: .5, Y: -.5}, Radius: 2},
	}
	for _, data := range fixtures {
		geometry := PrepareTerrainGeometry(data, 0)
		original := geometry.Collision
		var applied []RockCut
		recipe := geometry.Grid
		for _, hole := range holes {
			old := geometry.Topology
			cut, err := CutFromHole(hole)
			if err != nil {
				t.Fatal(err)
			}
			applied = append(applied, cut)
			geometry, _, _ = cut.Geometry(data.ID, geometry, 0)
			if !reflect.DeepEqual(geometry.Grid, recipe) || !reflect.DeepEqual(old.Grid, recipe) {
				t.Fatal("carve replaced material facets")
			}
			if len(recipe) > 0 && &geometry.Grid[0].Polygon[0] != &recipe[0].Polygon[0] {
				t.Fatal("material recipe was copied")
			}
			for y := GenerationMinY + .017; y < 2; y += .047 {
				for x := .017; x < 1; x += .047 {
					p := geom.V{X: x, Y: y + geometry.Top}
					want := original.Contains(p)
					for _, c := range applied {
						if (CollisionGeometry{Polygons: [][]geom.V{c.Poly}}).Contains(p) {
							want = false
							break
						}
					}
					if got := geometry.Collision.Contains(p); got != want {
						t.Fatalf("boundary subtraction changed occupancy at %v after %+v: %v/%v", p, hole, got, want)
					}
				}
			}
		}
	}
}
