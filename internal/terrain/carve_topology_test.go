package terrain

import (
	"reflect"
	"slices"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestCarveTopologyMatchesFullRebuild(t *testing.T) {
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
		for _, hole := range holes {
			old := geometry.Topology
			neighbors := make([][]int, len(old.neighbors))
			for i := range neighbors {
				neighbors[i] = slices.Clone(old.neighbors[i])
			}
			boundary := slices.Clone(old.allBoundary)
			cut, err := CutFromHole(hole)
			if err != nil {
				t.Fatal(err)
			}
			geometry, _, _ = cut.Geometry(data.ID, geometry, 0)
			if !reflect.DeepEqual(old.neighbors, neighbors) || !reflect.DeepEqual(old.allBoundary, boundary) {
				t.Fatal("carving modified published topology")
			}
			want := CarvedTopology(geometry.Grid, geometry.Cuts, geometry.Top)
			if !reflect.DeepEqual(geometry.Topology.neighbors, want.neighbors) ||
				!reflect.DeepEqual(geometry.Topology.allBoundary, want.allBoundary) ||
				!reflect.DeepEqual(geometry.Topology.Boundary, want.Boundary) {
				t.Fatalf("incremental topology differs from full rebuild after %+v", hole)
			}
			full := PrepareTerrainGeometry(SectionData{ID: data.ID, ForegroundTopology: want}, 0)
			if !reflect.DeepEqual(geometry.Blocks, full.Blocks) || !reflect.DeepEqual(geometry.Collision, full.Collision) {
				t.Fatal("incremental topology changed connectivity or collision loops")
			}
		}
	}
}
