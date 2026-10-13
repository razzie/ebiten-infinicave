package terrain

import (
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestDecorationSupportQueryReusesIndexAfterDeletion(t *testing.T) {
	data := SectionData{
		Foreground: RockGrid{terrainRect(.1, .2, .8, .6)},
		Mushrooms: []MushroomGroup{
			{Mushrooms: []Mushroom{{Anchor: geom.V{X: .2, Y: .4}}, {Anchor: geom.V{X: .205, Y: .4}}}},
			{Mushrooms: []Mushroom{{Anchor: geom.V{X: .8, Y: .4}}}},
		},
	}
	data.ForegroundTopology = NewRockTopology(data.Foreground)
	web := NewSpiderWeb([]geom.V{{X: .3, Y: .6}, {X: .5, Y: .6}, {X: .35, Y: .75}}, .5)
	data.ForegroundTopology.SpiderWebs = []SpiderWeb{web}
	original := PrepareTerrainGeometry(data, 0)
	cut, _ := CutFromHole(Hole{Shape: HoleCircle, Center: geom.V{X: .2, Y: -.6}, Radius: .01})
	updated, changed, removed := cut.PlantsWithChanges(original)
	if !changed || len(removed.Mushrooms) != 1 || len(removed.Webs) != 0 || len(updated.Vegetation.Mushrooms) != 1 {
		t.Fatal("multiple hit anchors did not produce one whole-group deletion")
	}
	if updated.decorations.index != original.decorations.index || original.decorations.dead[0] || len(original.Vegetation.Mushrooms) != 2 {
		t.Fatal("support query rebuilt its index or mutated the published source")
	}
	if repeated, changed, _ := cut.PlantsWithChanges(updated); changed || repeated != updated {
		t.Fatal("dead support was queried again")
	}
	free, _ := CutFromHole(Hole{Shape: HoleCircle, Center: web.Network.Nodes[web.Network.Hub[0]].Add(geom.V{Y: -1}), Radius: .001})
	if intact, changed, _ := free.PlantsWithChanges(updated); changed || intact != updated {
		t.Fatal("free silk damage removed a network")
	}
	cut, _ = CutFromHole(Hole{Shape: HoleCircle, Center: web.Corner.Add(geom.V{Y: -1}), Radius: .001})
	updated, changed, removed = cut.PlantsWithChanges(updated)
	if !changed || len(removed.Webs) != 1 || len(updated.Topology.SpiderWebs) != 0 || updated.decorations.index != original.decorations.index {
		t.Fatal("web support deletion lost its ID or rebuilt the index")
	}
	if !reflect.DeepEqual(original.Topology.SpiderWebs, []SpiderWeb{web}) {
		t.Fatal("web deletion changed the original topology")
	}
	miss, _ := CutFromHole(Hole{Shape: HoleCircle, Center: geom.V{X: .65, Y: -.3}, Radius: .01})
	if allocations := testing.AllocsPerRun(100, func() { miss.PlantsWithChanges(updated) }); allocations != 0 {
		t.Fatalf("support miss allocated %v times", allocations)
	}
}
