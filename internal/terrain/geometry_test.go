package terrain

import (
	"context"
	"image/color"
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

func TestRetiredGuidesSkipForegroundGeneration(t *testing.T) {
	guide := SplineGuide([]geom.V{{X: .2, Y: .3}, {X: .8, Y: .3}}, 1)
	loader := func(id int64) SectionContent {
		if id == 0 {
			return SectionContent{Guides: []Guide{guide}}
		}
		return SectionContent{}
	}
	retired := map[[32]byte]bool{GuideKey(guide.Pts, SectionTop(0)): true}
	for _, strategy := range []BranchingStrategy{BranchingSimple, BranchingRecursive, BranchingLoops} {
		t.Run(strategy.String(), func(t *testing.T) {
			builder := NewSectionBuilder(42, loader)
			builder.Shape.BranchingStrategy = strategy
			base := builder.Begin(context.Background(), 0)
			if len(base.branches) == 0 || len(base.data.ForegroundTopology.Grid) == 0 {
				t.Fatal("fixture has no branches or rock")
			}
			for _, id := range []int64{0, 1, 20, 0} {
				build := builder.BeginWithoutGuides(context.Background(), id, retired)
				if len(build.data.Guides) != 0 || len(build.branches) != 0 || len(build.raw) != 0 || len(build.data.ForegroundTopology.Grid) != 0 {
					t.Fatal("retired guide regenerated foreground data")
				}
			}
			if len(base.data.Guides) != 1 || len(base.branches) == 0 {
				t.Fatal("retirement mutated a published build")
			}
		})
	}
}

func TestGuideCacheRetirementSurvivesEviction(t *testing.T) {
	for _, mode := range []Orientation{Vertical, Horizontal} {
		cache := newGuideCache(42, mode)
		original := cache.window(0)
		retired := map[[32]byte]bool{GuideKey(original[0].Pts, SectionTop(0)): true}
		for _, id := range []int64{0, 1, 30, 0} {
			got := cache.windowWithoutGuides(id, retired)
			want := liveGuides(newGuideCache(42, mode).window(id), SectionTop(id), retired)
			if !reflect.DeepEqual(got, want) {
				t.Fatal("retirement changed surviving guide layout or depends on load order")
			}
			for owner, guides := range cache.resolved {
				for _, guide := range guides {
					if retired[GuideKey(guide.Pts, SectionTop(owner))] {
						t.Fatal("cache retained retired guide arrays")
					}
				}
			}
			for owner, guides := range cache.proposals {
				for _, guide := range guides {
					if retired[GuideKey(guide.Pts, SectionTop(owner))] {
						t.Fatal("proposal cache retained retired guide arrays")
					}
				}
			}
		}
	}
}

func TestGuideRetirementDoesNotUseSeedAsIdentity(t *testing.T) {
	a := SplineGuide([]geom.V{{X: .1, Y: .2}, {X: .3, Y: .2}}, 1)
	b := SplineGuide([]geom.V{{X: .7, Y: .6}, {X: .9, Y: .6}}, 1)
	a.Seed, b.Seed = 17, 17
	h := PrepareTerrainGeometry(SectionData{Guides: []Guide{a, b}, Foreground: RockGrid{
		terrainRect(.1, .2, .2, .1), terrainRect(.7, .6, .2, .1),
	}}, 0)
	retired := map[[32]byte]bool{GuideKey(a.Pts, h.Top): true}
	live := h.WithoutGuides(retired)
	if len(live.Guides) != 1 || !reflect.DeepEqual(live.Guides[0], b) || len(h.Guides) != 2 {
		t.Fatal("retirement conflated authored guides with equal seeds or mutated source")
	}
}

func BenchmarkRetiredGuideGeneration(b *testing.B) {
	guide := SplineGuide([]geom.V{{X: .2, Y: .3}, {X: .8, Y: .3}}, 1)
	loader := func(id int64) SectionContent {
		if id == 0 {
			return SectionContent{Guides: []Guide{guide}}
		}
		return SectionContent{}
	}
	for _, name := range []string{"original", "retired"} {
		b.Run(name, func(b *testing.B) {
			builder := NewSectionBuilder(42, loader)
			var retired map[[32]byte]bool
			if name == "retired" {
				retired = map[[32]byte]bool{GuideKey(guide.Pts, SectionTop(0)): true}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				builder.BeginWithoutGuides(context.Background(), 0, retired)
			}
		})
	}
}

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
