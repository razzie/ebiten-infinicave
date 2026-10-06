package infinicave_test

import (
	"math"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave"
)

func TestPublicSectionGeneration(t *testing.T) {
	if _, err := infinicave.GenerateSection(42, -1); err == nil {
		t.Fatal("negative section ID accepted")
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
	// A caller modifying generated geometry must not corrupt other generations.
	before := b.Foreground[0].Polygon[0]
	a.Foreground[0].Polygon[0].X += 100
	if b.Foreground[0].Polygon[0] != before {
		t.Fatal("generated sections share mutable geometry")
	}
	approximate, err := infinicave.GenerateSectionWithConfig(infinicave.Config{Seed: 42, CollisionTolerance: 2}, 0)
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
	t.Logf("collision vertices at tolerance 0 / 2: %d / %d", exactCount, approximateCount)
}

func TestPublicSceneConfigurationAndLifecycle(t *testing.T) {
	for _, config := range []infinicave.Config{
		{Texture: math.NaN()}, {Texture: math.Inf(1)}, {Texture: -1},
		{Texture: 17}, {Study: infinicave.Study(-1)}, {Study: infinicave.Study(999)},
		{View: infinicave.View(-1)}, {View: infinicave.View(999)},
		{CollisionTolerance: -1}, {CollisionTolerance: math.NaN()}, {CollisionTolerance: math.Inf(1)},
	} {
		if scene, err := infinicave.NewScene(config); err == nil {
			scene.Close()
			t.Fatalf("invalid configuration accepted: %+v", config)
		}
		if _, err := infinicave.GenerateSectionWithConfig(config, 0); err == nil {
			t.Fatalf("generator accepted invalid configuration: %+v", config)
		}
	}
	for _, config := range []infinicave.Config{{}, infinicave.DefaultConfig(), {Study: infinicave.StudyCurl, View: infinicave.ViewNormals, Texture: 8}} {
		scene, err := infinicave.NewScene(config)
		if err != nil {
			t.Fatal(err)
		}
		for _, viewport := range []infinicave.Viewport{
			{}, {Y: math.NaN(), Height: 800}, {Y: -800, Height: -1},
			{Y: -799, Height: 800}, {Y: -800, Height: 800, Velocity: math.Inf(1)},
		} {
			if scene.Update(viewport) {
				t.Fatalf("invalid viewport reported ready: %+v", viewport)
			}
		}
		scene.Reset(17)
		scene.Close()
		scene.Close()
		scene.Reset(42)
		if scene.Update(infinicave.Viewport{Y: -800, Height: 800}) {
			t.Fatal("closed scene reported ready")
		}
		// Closed scenes must leave rendering destinations untouched.
		scene.Draw(nil, infinicave.Viewport{Y: -800, Height: 800})
		scene.DrawHover(nil, infinicave.Viewport{Y: -800, Height: 800}, 1, 1)
	}
}
