package infinicave

import (
	"math/rand"
	"reflect"
	"testing"
)

func TestBackgroundMaterialDoesNotBakeForegroundShadows(t *testing.T) {
	background := RockGrid{{Center: V{.5, .55}, Polygon: []V{{0, -1}, {1, -1}, {1, 2}, {0, 2}}, Normal: V3{Z: 1}}}
	foreground := RockGrid{{Center: V{.5, .5}, Polygon: []V{{.45, .45}, {.55, .45}, {.55, .53}, {.45, .53}}, Z: .07, Normal: V3{Z: 1}, Raised: true}}
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	shadeRockGrids(background, foreground, noise)
	withRock := background[0]
	shadeRockGrids(background, nil, noise)
	if !reflect.DeepEqual(withRock, background[0]) {
		t.Fatal("foreground silhouette changed the cached background material")
	}
	if background[0].Color != backgroundSurfaceColor(background[0].Center, noise, background[0].Normal) {
		t.Fatal("background still contains baked cast-shadow darkening")
	}
}

func TestBackgroundConfigurationAndLifecycle(t *testing.T) {
	configs := []Config{{}, DefaultConfig(), {BackgroundBlur: .003}, {ShadowOpacity: .5}, {ShadowBlur: .01}}
	for view := ViewClay; view <= ViewShadows; view++ {
		configs = append(configs, Config{BackgroundBlur: .003, ShadowOpacity: .5, View: view})
	}
	for _, config := range configs {
		scene, err := NewScene(config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(scene.Close)
		enabled := config.View == ViewShaded && (config.BackgroundBlur > 0 || config.ShadowOpacity > 0)
		if (scene.background != nil) != enabled {
			t.Fatalf("background effects enabled incorrectly for %+v", config)
		}
		before := scene.background
		scene.Reset(42)
		if scene.background != before {
			t.Fatal("world reset lost background rendering settings")
		}
		scene.Close()
		scene.Close()
	}
}
