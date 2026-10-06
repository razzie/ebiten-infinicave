package infinicave

import (
	"math"
	"reflect"
	"testing"
	"time"
)

func TestLoadedGuidesKeepWorldPositionsAndCallerOwnership(t *testing.T) {
	source := []Guide{{
		Pts: []V{{.2, .4}, {.2, .4}, {.7, .5}}, S: []float64{99},
		Min: V{-99, -99}, Max: V{99, 99}, BrightSign: -1,
	}}
	before := Guide{
		Pts: append([]V(nil), source[0].Pts...), S: append([]float64(nil), source[0].S...),
		Min: source[0].Min, Max: source[0].Max, BrightSign: -1,
	}
	local, ok := loadedGuide(source[0])
	if !ok || !reflect.DeepEqual(local.Pts, []V{{.2, .4}, {.7, .5}}) || local.Min != (V{.2, .4}) || local.Max != (V{.7, .5}) {
		t.Fatal("internal loader changed scene-unit coordinates")
	}
	var requested []int64
	load := func(id int64) SectionContent {
		requested = append(requested, id)
		return SectionContent{Guides: source}
	}
	a, b := loadedWorldContent(42, 0, load).Guides, loadedWorldContent(42, 1, load).Guides
	for _, id := range requested {
		if id > 0 {
			t.Fatalf("loader requested a section below the floor: %d", id)
		}
	}
	if !reflect.DeepEqual(requested[:3], []int64{-2, -1, 0}) {
		t.Fatalf("bottom window requested incorrect IDs: %v", requested)
	}
	bySeed := make(map[int64]Guide)
	for _, g := range a {
		g = shiftedGuide(g, sectionTop(0))
		bySeed[g.Seed] = g
		if len(g.Pts) != 2 || g.BrightSign != -1 || math.Abs(g.S[1]-math.Hypot(.500, .100)) > 1e-9 {
			t.Fatal("loaded guide has incorrect points, lighting, or arc length")
		}
	}
	shared := 0
	for _, g := range b {
		other, ok := bySeed[g.Seed]
		if !ok {
			continue
		}
		shared++
		world := shiftedGuide(g, sectionTop(1))
		if world.Seed != other.Seed || world.BrightSign != other.BrightSign || len(world.Pts) != len(other.Pts) {
			t.Fatal("neighboring windows changed loaded guide metadata")
		}
		for i, p := range world.Pts {
			if p.Sub(other.Pts[i]).Len() > 1e-12 || math.Abs(world.S[i]-other.S[i]) > 1e-12 {
				t.Fatal("neighboring windows changed loaded guide geometry")
			}
		}
	}
	if shared != 3 {
		t.Fatalf("neighboring windows shared %d guides, want 3", shared)
	}
	a[0].Pts[0].X += 100
	a[0].S[1] += 100
	if !reflect.DeepEqual(source[0], before) {
		t.Fatal("generation modified callback-owned guide data")
	}
}

func TestLoadedGuidesAllowEmptySectionsAndSkipInvalidPolylines(t *testing.T) {
	for _, guides := range [][]Guide{
		nil, {}, {{Pts: []V{{.5, .5}}}},
		{{Pts: []V{{.5, .5}, {.5, .5}}}},
		{{Pts: []V{{.2, .4}, {math.NaN(), .5}}}},
		{{Pts: []V{{.2, .4}, {.5, math.Inf(1)}}}},
	} {
		loaded := loadedWorldContent(42, 0, func(int64) SectionContent { return SectionContent{Guides: guides} }).Guides
		if len(loaded) != 0 {
			t.Fatal("empty or invalid polylines unexpectedly generated guides")
		}
	}
	data := buildSectionMode(42, 0, StudyCurl, func(int64) SectionContent { return SectionContent{} })
	if len(data.guides) != 0 || len(data.foreground) != 0 || len(data.background) == 0 {
		t.Fatal("empty loader did not override procedural foreground generation")
	}
}

func TestSceneUsesSectionLoaderAfterReset(t *testing.T) {
	scene, err := NewScene(Config{
		Seed: 42, Study: StudyCurl, View: ViewClay,
		LoadSection: func(id int64) SectionContent {
			if id != -1 {
				return SectionContent{}
			}
			return SectionContent{Guides: []Guide{{Pts: []V{{.2, .4}, {.7, .5}}, Seed: 1234}}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer scene.Close()
	checkWorker := func() {
		t.Helper()
		scene.world.request(1)
		select {
		case mesh := <-scene.world.results:
			if mesh.id != 1 || mesh.geometry == nil || len(mesh.geometry.guides) != 1 || mesh.geometry.guides[0].Seed != 1234 || len(mesh.geometry.collision.Polygons) == 0 {
				t.Fatal("scene worker did not use the configured guide loader")
			}
		case <-time.After(20 * time.Second):
			t.Fatal("custom guide generation stalled")
		}
	}
	checkWorker()
	scene.Reset(7)
	checkWorker()
}
