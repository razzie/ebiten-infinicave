package infinicave_test

import (
	"math"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave"
)

func TestPublicAntialiasingAndReadiness(t *testing.T) {
	for _, mode := range []infinicave.AntialiasingMode{infinicave.AntialiasingFull, infinicave.AntialiasingOff, infinicave.AntialiasingFXAA} {
		parsed, err := infinicave.ParseAntialiasingMode(mode.String())
		if err != nil || parsed != mode {
			t.Fatalf("public antialiasing mode %s: %v", mode, err)
		}
		scene, err := infinicave.NewScene(infinicave.Config{Antialiasing: mode, DisableBackground: true})
		if err != nil {
			t.Fatal(err)
		}
		ready := scene.Readiness(infinicave.Viewport{Y: -1, Height: 1})
		if !ready.Background || ready.Foreground || ready.Collision {
			t.Fatalf("public readiness: %+v", ready)
		}
		scene.Close()
	}
}

func TestPublicCarveTerrain(t *testing.T) {
	scene, err := infinicave.NewScene(infinicave.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer scene.Close()
	hole := infinicave.Hole{Shape: infinicave.HoleCircle, Center: infinicave.V{X: .5, Y: -.5}, Radius: .05}
	result, err := scene.CarveTerrain(hole)
	if err != nil || result.Complete || len(result.Changes) != 0 || len(result.SectionIDs) != 0 {
		t.Fatalf("terrain cut before loading: %+v / %v", result, err)
	}
	hole.Radius = math.NaN()
	if _, err := scene.CarveTerrain(hole); err == nil {
		t.Fatal("terrain cut accepted an invalid radius")
	}
	hole.Radius = .05
	scene.Close()
	if _, err := scene.CarveTerrain(hole); err == nil {
		t.Fatal("closed scene accepted a terrain cut")
	}
}

func TestPublicStreamingConfiguration(t *testing.T) {
	defaults := infinicave.DefaultConfig()
	if defaults.RetainSections != 3 || defaults.LookaheadSections != 4 {
		t.Fatalf("unexpected streaming defaults: %d / %d", defaults.RetainSections, defaults.LookaheadSections)
	}
	for _, config := range []infinicave.Config{{RetainSections: -1}, {LookaheadSections: -1}} {
		if scene, err := infinicave.NewScene(config); err == nil {
			scene.Close()
			t.Fatalf("accepted negative streaming counts: %+v", config)
		}
	}
}

func TestPublicAppearanceFadeConfiguration(t *testing.T) {
	if !infinicave.DefaultConfig().AppearanceFade {
		t.Fatal("default appearance fade is disabled")
	}
	for _, enabled := range []bool{false, true} {
		scene, err := infinicave.NewScene(infinicave.Config{AppearanceFade: enabled})
		if err != nil {
			t.Fatal(err)
		}
		scene.Reset(42)
		scene.Close()
	}
}

func TestPublicSectionGeneration(t *testing.T) {
	if _, err := infinicave.GenerateSection(42, 1); err == nil {
		t.Fatal("positive section ID accepted")
	}
	a, err := infinicave.GenerateSection(42, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := infinicave.GenerateSectionWithConfig(infinicave.Config{Seed: 42}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("public generation does not reproduce the same section")
	}
	if a.ID != 0 || a.Top != -float64(infinicave.SectionHeight) || a.WindowTop != 2*a.Top {
		t.Fatalf("incorrect world coordinate metadata: %d, %v, %v", a.ID, a.Top, a.WindowTop)
	}
	if len(a.Background) == 0 || len(a.Foreground) == 0 || len(a.Guides) == 0 || len(a.Collision.Polygons) == 0 {
		t.Fatal("public generation omitted terrain")
	}
	checkSectionUnits(t, a)
	// A caller modifying generated geometry must not corrupt other generations.
	before := b.Foreground[0].Polygon[0]
	a.Foreground[0].Polygon[0].X += .1
	if b.Foreground[0].Polygon[0] != before {
		t.Fatal("generated sections share mutable geometry")
	}
	approximate, err := infinicave.GenerateSectionWithConfig(infinicave.Config{Seed: 42, CollisionTolerance: .002}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(approximate.Foreground, b.Foreground) || !reflect.DeepEqual(approximate.Background, b.Background) {
		t.Fatal("collision tolerance changed visual terrain")
	}
	vertices := func(geometry infinicave.CollisionGeometry) int {
		count := 0
		for _, poly := range geometry.Polygons {
			count += len(poly)
		}
		return count
	}
	exactCount, approximateCount := vertices(b.Collision), vertices(approximate.Collision)
	if approximateCount >= exactCount {
		t.Fatalf("tolerance did not reduce generated collision vertices: %d >= %d", approximateCount, exactCount)
	}
	t.Logf("collision vertices at tolerance 0 / .002: %d / %d", exactCount, approximateCount)
}

func TestPublicSectionSectionLoaderAndNegativeID(t *testing.T) {
	var requested []int64
	section, err := infinicave.GenerateSectionWithConfig(infinicave.Config{
		Seed: 42,
		LoadSection: func(id int64) infinicave.SectionContent {
			requested = append(requested, id)
			if id != -1 {
				return infinicave.SectionContent{}
			}
			return infinicave.SectionContent{Guides: []infinicave.Guide{{Pts: []infinicave.V{{X: .2, Y: .4}, {X: .7, Y: .5}}}}}
		},
	}, -1)
	if err != nil {
		t.Fatal(err)
	}
	if section.ID != -1 || section.Top != -2 || section.WindowTop != -3 || section.Collision.ID != -1 {
		t.Fatalf("incorrect negative ID metadata: section %d, top %v, window %v, collision %d", section.ID, section.Top, section.WindowTop, section.Collision.ID)
	}
	if !reflect.DeepEqual(requested, []int64{-3, -2, -1, 0}) {
		t.Fatalf("unexpected loader IDs: %v", requested)
	}
	if len(section.Guides) != 1 || section.Guides[0].BrightSign != 1 || section.Guides[0].Seed == 0 {
		t.Fatal("custom guide was omitted or procedural guides were added")
	}
	for i, want := range []infinicave.V{{X: .2, Y: .4}, {X: .7, Y: .5}} {
		if section.Guides[0].Pts[i].Sub(want).Len() > 1e-12 {
			t.Fatal("custom guide coordinates do not use section-local scene units")
		}
	}
	if len(section.Foreground) == 0 || len(section.Collision.Polygons) == 0 {
		t.Fatal("custom guides did not produce rock and collision geometry")
	}
	checkSectionUnits(t, section)
}

func checkSectionUnits(t *testing.T, section infinicave.Section) {
	t.Helper()
	if infinicave.Width != 1 || infinicave.SectionHeight != 1 {
		t.Fatal("sections must be unit squares")
	}
	// Padding limits the generation window; the shared formations need not
	// fill its entire extent, especially with an authored section loader.
	for layer, grid := range []infinicave.RockGrid{section.Background, section.Foreground} {
		minX, maxX := 0.0, 1.0
		if layer == 0 {
			minX, maxX = -.5, 1.5
		}
		for _, cell := range grid {
			if math.Abs(cell.Z) > 1 {
				t.Fatalf("rock height is outside scene scale: %v", cell.Z)
			}
			for _, p := range cell.Polygon {
				if p.X < minX-1e-9 || p.X > maxX+1e-9 || p.Y < -1-1e-9 || p.Y > 2+1e-9 {
					t.Fatalf("rock vertex is outside the padded unit section: %v", p)
				}
			}
		}
	}
	for _, guide := range section.Guides {
		length := 0.0
		for i := 1; i < len(guide.Pts); i++ {
			length += guide.Pts[i].Sub(guide.Pts[i-1]).Len()
			if math.Abs(guide.S[i]-length) > 1e-9 {
				t.Fatal("guide arc length uses different units from its points")
			}
		}
	}
	for layer, vines := range [][]infinicave.Vine{section.Vines, section.ForegroundVines} {
		minX, maxX := 0.0, 1.0
		if layer == 0 {
			minX, maxX = -.5, 1.5
		}
		for _, vine := range vines {
			for _, point := range vine.Points {
				if point.Radius < 0 || point.Radius > .01 || point.P.X < minX || point.P.X > maxX {
					t.Fatalf("vine point uses incorrect scene units: %+v", point)
				}
			}
		}
	}
	for _, group := range section.Mushrooms {
		for _, mushroom := range group.Mushrooms {
			if mushroom.CapWidth <= 0 || mushroom.CapWidth > .1 || mushroom.CapHeight <= 0 || mushroom.CapHeight > .1 {
				t.Fatalf("mushroom dimensions use incorrect scene units: %v by %v", mushroom.CapWidth, mushroom.CapHeight)
			}
			if math.Abs(mushroom.RootDirection.Len()-1) > 1e-9 {
				t.Fatal("normalization changed a mushroom's unit direction")
			}
		}
	}
	if section.Collision.Top != section.Top {
		t.Fatal("collision metadata uses different units from section metadata")
	}
	// Public collision points are world coordinates; rock faces are local to
	// the unit square. Test their agreement throughout the owned section.
	for y := .011; y < 1; y += .041 {
		for x := .019; x < .982; x += .041 {
			p := infinicave.V{X: x, Y: y}
			inside := false
			for _, cell := range section.Foreground {
				inside = inside || (infinicave.CollisionGeometry{Polygons: [][]infinicave.V{cell.Polygon}}).Contains(p)
			}
			if section.Collision.Contains(p.Add(infinicave.V{Y: section.Top})) != inside {
				t.Fatalf("world collision and local rock faces disagree at %v", p)
			}
		}
	}
}

func TestPublicSceneConfigurationAndLifecycle(t *testing.T) {
	if infinicave.DefaultConfig().DisableBackground {
		t.Fatal("default configuration disabled background rendering")
	}
	for _, config := range []infinicave.Config{
		{Texture: math.NaN()}, {Texture: math.Inf(1)}, {Texture: -1},
		{Texture: 17},
		{BackgroundBlur: -1}, {BackgroundBlur: .051}, {BackgroundBlur: math.NaN()}, {BackgroundBlur: math.Inf(1)},
		{ShadowBlur: -1}, {ShadowBlur: .051}, {ShadowBlur: math.NaN()}, {ShadowBlur: math.Inf(1)},
		{ShadowOpacity: -1}, {ShadowOpacity: 1.01}, {ShadowOpacity: math.NaN()}, {ShadowOpacity: math.Inf(1)},
		{Fog: -1}, {Fog: math.NaN()}, {Fog: math.Inf(1)}, {Fog: math.Inf(-1)},
		{ShadowOffset: infinicave.V{X: 1.01}}, {ShadowOffset: infinicave.V{Y: -1.01}},
		{ShadowOffset: infinicave.V{X: math.NaN()}}, {ShadowOffset: infinicave.V{Y: math.Inf(1)}},
		{View: infinicave.View(-1)}, {View: infinicave.View(999)},
		{CollisionTolerance: -1}, {CollisionTolerance: math.NaN()}, {CollisionTolerance: math.Inf(1)},
		{MushroomDensity: -1}, {MushroomDensity: 1.01}, {MushroomDensity: math.NaN()}, {MushroomDensity: math.Inf(1)}, {MushroomDensity: math.Inf(-1)},
		{MossDensity: -1}, {MossDensity: 1.01}, {MossDensity: math.NaN()}, {MossDensity: math.Inf(1)}, {MossDensity: math.Inf(-1)},
		{GrassDensity: -1}, {GrassDensity: 1.01}, {GrassDensity: math.NaN()}, {GrassDensity: math.Inf(1)}, {GrassDensity: math.Inf(-1)},
		{HangingGrowthDensity: -1}, {HangingGrowthDensity: 1.01}, {HangingGrowthDensity: math.NaN()}, {HangingGrowthDensity: math.Inf(1)}, {HangingGrowthDensity: math.Inf(-1)},
		{SpiderWebDensity: -1}, {SpiderWebDensity: 1.01}, {SpiderWebDensity: math.NaN()}, {SpiderWebDensity: math.Inf(1)}, {SpiderWebDensity: math.Inf(-1)},
		{SpiderWebMaxSize: -1}, {SpiderWebMaxSize: math.NaN()}, {SpiderWebMaxSize: math.Inf(1)}, {SpiderWebMaxSize: math.Inf(-1)},
		{BatsPerMinute: -1}, {BatsPerMinute: math.NaN()}, {BatsPerMinute: math.Inf(1)}, {BatsPerMinute: math.Inf(-1)},
	} {
		if scene, err := infinicave.NewScene(config); err == nil {
			scene.Close()
			t.Fatalf("invalid configuration accepted: %+v", config)
		}
		if _, err := infinicave.GenerateSectionWithConfig(config, 0); err == nil {
			t.Fatalf("generator accepted invalid configuration: %+v", config)
		}
	}
	for _, config := range []infinicave.Config{{}, infinicave.DefaultConfig(), {View: infinicave.ViewNormals, Texture: 8}, {DisableBackground: true}} {
		scene, err := infinicave.NewScene(config)
		if err != nil {
			t.Fatal(err)
		}
		for _, viewport := range []infinicave.Viewport{
			{}, {Y: -1, Height: math.NaN()}, {Y: -1, Height: math.Inf(1)}, {Y: math.NaN(), Height: .8}, {Y: -.8, Height: -1},
			{Y: -.799, Height: .8}, {Y: -.8, Height: .8, Velocity: math.Inf(1)},
		} {
			if scene.Update(viewport) {
				t.Fatalf("invalid viewport reported ready: %+v", viewport)
			}
		}
		scene.Reset(17)
		scene.Close()
		scene.Close()
		scene.Reset(42)
		if scene.Update(infinicave.Viewport{Y: -.8, Height: .8}) {
			t.Fatal("closed scene reported ready")
		}
		// Closed scenes must leave rendering destinations untouched.
		scene.Draw(nil, infinicave.Viewport{Y: -.8, Height: .8})
	}
}
