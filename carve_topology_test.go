package infinicave

import (
	"reflect"
	"slices"
	"testing"
)

func TestCarveTopologyMatchesFullRebuild(t *testing.T) {
	fixtures := []sectionData{
		buildSection(42, 0),
		{foreground: RockGrid{
			terrainRock([]V{{.1, -.2}, {.5, -.2}, {.5, .3}, {.3, .3}, {.3, .8}, {.1, .8}}),
			terrainRect(.3, .3, .2, .5), terrainRect(.5, -.2, .2, 1),
			terrainRect(0, .8, 1, 1.2),
		}},
	}
	holes := []Hole{
		{Shape: HoleCircle, Center: V{.4, -.95}, Radius: .12},
		{Shape: HoleSegment, Start: V{.2, -.85}, End: V{.7, -1.15}, Width: .035},
		{Shape: HoleCircle, Center: V{.42, -1.08}, Radius: .06},
		{Shape: HoleSegment, Start: V{.1, -.5}, End: V{.9, -.5}, Width: .05},
		{Shape: HoleCircle, Center: V{.5, -.5}, Radius: 2},
	}
	for _, data := range fixtures {
		geometry := prepareTerrainGeometry(data, 0)
		for _, hole := range holes {
			old := geometry.topology
			neighbors := make([][]int, len(old.neighbors))
			for i := range neighbors {
				neighbors[i] = slices.Clone(old.neighbors[i])
			}
			boundary := slices.Clone(old.allBoundary)
			cut, err := hole.rockCut()
			if err != nil {
				t.Fatal(err)
			}
			geometry, _, _ = cut.geometry(data.id, geometry, 0)
			if !reflect.DeepEqual(old.neighbors, neighbors) || !reflect.DeepEqual(old.allBoundary, boundary) {
				t.Fatal("carving modified published topology")
			}
			want := carvedTopology(geometry.grid, geometry.cuts, geometry.top)
			if !reflect.DeepEqual(geometry.topology.neighbors, want.neighbors) ||
				!reflect.DeepEqual(geometry.topology.allBoundary, want.allBoundary) ||
				!reflect.DeepEqual(geometry.topology.boundary, want.boundary) {
				t.Fatalf("incremental topology differs from full rebuild after %+v", hole)
			}
			full := prepareTerrainGeometry(sectionData{id: data.id, foregroundTopology: want}, 0)
			if !reflect.DeepEqual(geometry.blocks, full.blocks) || !reflect.DeepEqual(geometry.collision, full.collision) {
				t.Fatal("incremental topology changed connectivity or collision loops")
			}
		}
	}
}
