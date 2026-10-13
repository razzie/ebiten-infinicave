package terrain

import (
	"context"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

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
