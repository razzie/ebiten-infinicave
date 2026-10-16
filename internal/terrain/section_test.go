package terrain

import (
	"math"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestLoadedGuidesKeepWorldPositionsAndCallerOwnership(t *testing.T) {
	source := []Guide{{
		Pts: []geom.V{{X: .2, Y: .4}, {X: .2, Y: .4}, {X: .7, Y: .5}}, S: []float64{99},
		Min: geom.V{X: -99, Y: -99}, Max: geom.V{X: 99, Y: 99}, BrightSign: -1,
	}}
	before := Guide{
		Pts: append([]geom.V(nil), source[0].Pts...), S: append([]float64(nil), source[0].S...),
		Min: source[0].Min, Max: source[0].Max, BrightSign: -1,
	}
	local, ok := loadedGuide(source[0])
	if !ok || !reflect.DeepEqual(local.Pts, []geom.V{{X: .2, Y: .4}, {X: .7, Y: .5}}) || local.Min != (geom.V{X: .2, Y: .4}) || local.Max != (geom.V{X: .7, Y: .5}) {
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
		g = shiftedGuide(g, SectionTop(0))
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
		world := shiftedGuide(g, SectionTop(1))
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
		nil, {}, {{Pts: []geom.V{{X: .5, Y: .5}}}},
		{{Pts: []geom.V{{X: .5, Y: .5}, {X: .5, Y: .5}}}},
		{{Pts: []geom.V{{X: .2, Y: .4}, {X: math.NaN(), Y: .5}}}},
		{{Pts: []geom.V{{X: .2, Y: .4}, {X: .5, Y: math.Inf(1)}}}},
	} {
		loaded := loadedWorldContent(42, 0, func(int64) SectionContent { return SectionContent{Guides: guides} }).Guides
		if len(loaded) != 0 {
			t.Fatal("empty or invalid polylines unexpectedly generated guides")
		}
	}
	data := NewSectionBuilder(42, func(int64) SectionContent { return SectionContent{} }).Build(0)
	if len(data.Guides) != 0 || len(data.Foreground) != 0 || len(data.Background) == 0 {
		t.Fatal("empty loader did not override procedural foreground generation")
	}
}

func TestSectionLoaderHoleCoordinatesAndOwnership(t *testing.T) {
	source := SectionContent{
		Guides: []Guide{{Pts: []geom.V{{X: .2, Y: .9}, {X: .7, Y: 1.1}}}},
		Holes: []Hole{
			{Shape: HoleCircle, Center: geom.V{X: .4, Y: 1}, Radius: .06},
			{Shape: HoleSegment, Start: geom.V{X: .2, Y: .9}, End: geom.V{X: .7, Y: 1.1}, Width: .02},
			{Shape: HoleCircle, Center: geom.V{X: .5, Y: .5}, Radius: math.NaN()},
			{Shape: HoleCircle, Center: geom.V{X: .5, Y: .5}, Radius: 2},
			{Shape: HoleShape(255)},
		},
	}
	before := append([]Hole(nil), source.Holes...)
	loader := func(id int64) SectionContent {
		if id == -1 {
			return source
		}
		return SectionContent{}
	}
	a, b := loadedWorldContent(42, 0, loader), loadedWorldContent(42, 1, loader)
	if len(a.Holes) != 2 || len(b.Holes) != 2 {
		t.Fatal("invalid holes were not filtered")
	}
	for i := range a.Holes {
		aHole, bHole := TranslateHoleY(a.Holes[i], SectionTop(0)), TranslateHoleY(b.Holes[i], SectionTop(1))
		if aHole != bHole {
			t.Fatalf("neighboring hole positions differ: %+v / %+v", aHole, bHole)
		}
	}
	a.Holes[0].Center.X = 99
	for i := range before {
		if source.Holes[i].Shape != before[i].Shape || source.Holes[i].Center != before[i].Center || source.Holes[i].Width != before[i].Width {
			t.Fatal("loader-owned hole was modified")
		}
	}
}
