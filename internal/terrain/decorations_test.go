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

func TestCarveVinesRetainsOriginalGeometryWithoutDetection(t *testing.T) {
	cut, _ := CutFromHole(Hole{Shape: HoleSegment, Start: geom.V{X: .3, Y: -.4}, End: geom.V{X: .7, Y: -.4}, Width: .2})
	v := Vine{Parent: -1, Points: []VinePoint{{geom.V{X: .1, Y: .5}, .01}, {geom.V{X: .9, Y: .5}, .01}}}
	source := Vegetation{Vines: []Vine{v}, ForegroundVines: []Vine{v}}
	plants, changed := cut.vegetation(source, -1)
	if changed || !reflect.DeepEqual(plants, source) || &plants.Vines[0] != &source.Vines[0] || &plants.ForegroundVines[0] != &source.ForegroundVines[0] {
		t.Fatal("texture-only carving changed original vine recipes")
	}
	if allocs := testing.AllocsPerRun(10, func() { cut.vegetation(source, -1) }); allocs != 0 {
		t.Fatalf("vine-only edit allocated %v times", allocs)
	}
}

func TestCarveMushroomsUsesGuideRootAndExactCut(t *testing.T) {
	const top = -3.0
	m := Mushroom{Anchor: geom.V{X: .5, Y: .4}, Stem: []geom.V{{X: .5, Y: .404}, {X: .5, Y: .38}}, CapCenter: geom.V{X: .5, Y: .38}}
	source := Vegetation{Mushrooms: []MushroomGroup{{Mushrooms: []Mushroom{m}}}}
	for _, tc := range []struct {
		name   string
		center geom.V
		radius float64
		hit    bool
	}{
		{"guide root", m.Anchor, .001, true},
		{"buried stalk only", m.Stem[0], .001, false},
		{"stalk end", m.CapCenter, .003, false},
		{"bounds overlap outside circle", m.Anchor.Add(geom.V{X: .009, Y: .009}), .01, false},
		{"root on cut boundary", m.Anchor.Add(geom.V{X: .01}), .01, false},
		{"distant cut", geom.V{X: .8, Y: .8}, .01, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cut, err := CutFromHole(Hole{Shape: HoleCircle, Center: tc.center.Add(geom.V{Y: top}), Radius: tc.radius})
			if err != nil {
				t.Fatal(err)
			}
			plants, changed := cut.vegetation(source, top)
			if changed != tc.hit || (len(plants.Mushrooms) == 0) != tc.hit {
				t.Fatalf("root damage: changed=%v mushrooms=%d, want hit=%v", changed, len(plants.Mushrooms), tc.hit)
			}
			if !tc.hit && !reflect.DeepEqual(plants, source) {
				t.Fatal("missed root changed vegetation")
			}
			if len(source.Mushrooms[0].Mushrooms) != 1 || !reflect.DeepEqual(source.Mushrooms[0].Mushrooms[0], m) {
				t.Fatal("carving mutated source mushroom")
			}
			if !tc.hit {
				if allocs := testing.AllocsPerRun(10, func() { cut.vegetation(source, top) }); allocs != 0 {
					t.Fatalf("missed root allocated %v times", allocs)
				}
			}
		})
	}
}

func TestInsetRockAnchorHandlesThinRockAndConcaveCorners(t *testing.T) {
	for _, tc := range []struct {
		name      string
		poly      []geom.V
		p, inward geom.V
	}{
		{"thin wall", []geom.V{{}, {X: .01}, {X: .01, Y: .00001}, {Y: .00001}}, geom.V{X: .005}, geom.V{Y: 1}},
		{"convex corner", []geom.V{{}, {X: .01}, {X: .01, Y: .01}, {Y: .01}}, geom.V{}, geom.V{}},
		{"concave wall", []geom.V{{}, {X: .01}, {X: .01, Y: .01}, {X: .005, Y: .005}, {Y: .01}}, geom.V{X: .005, Y: .005}, geom.V{Y: -1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, ok := InsetRockAnchor(tc.poly, tc.p, tc.inward)
			if !ok || !geom.InsidePolygon(p, tc.poly) || p.Sub(tc.p).Len() > .001 {
				t.Fatalf("anchor is not just inside its support: %+v, ok=%v", p, ok)
			}
		})
	}
	if _, ok := InsetRockAnchor([]geom.V{{}, {X: .01}, {X: .01, Y: .01}, {Y: .01}}, geom.V{X: .1}, geom.V{}); ok {
		t.Fatal("distant rock acquired an anchor")
	}
}

func TestGeneratedDecorationSupportsStayInsideRock(t *testing.T) {
	for _, mode := range []Orientation{Vertical, Horizontal} {
		data := NewSectionBuilder(42, nil, mode).Build(0)
		inside := func(p geom.V, grid RockGrid) bool {
			for _, c := range grid {
				if geom.InsidePolygon(p, c.Polygon) {
					return true
				}
			}
			return false
		}
		for _, group := range data.Mushrooms {
			for _, m := range group.Mushrooms {
				if !m.supportSet || !inside(MushroomSupport(m), data.ForegroundBake.Grid) {
					t.Fatalf("%s mushroom has no rock-interior support", mode)
				}
			}
		}
		for _, web := range data.ForegroundTopology.SpiderWebs {
			if len(web.supports) != web.Network.Anchors {
				t.Fatal("web lost a rock attachment")
			}
			for _, p := range SpiderWebSupports(web) {
				if !inside(p, data.ForegroundTopology.Appearance) {
					t.Fatal("web support is outside its supporting rock")
				}
			}
		}
	}
}
