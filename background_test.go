package infinicave

import (
	"math"
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

func TestExtendedBackgroundVoronoiCoverage(t *testing.T) {
	seeds := []V{{-.4, -.5}, {-.2, 1.4}, {.3, .4}, {.7, -.4}, {1.2, .3}, {1.4, 1.6}}
	cells := voronoiCellsInRange(seeds, backgroundMinX, backgroundMaxX)
	area := 0.0
	for i, cell := range cells {
		// Compare the bounded search with clipping against every site.
		want := []V{{-.5, -1}, {1.5, -1}, {1.5, 2}, {-.5, 2}}
		for j, other := range seeds {
			if i != j {
				want = clipHalfPlane(want, other.Sub(seeds[i]), .5*(other.Len2()-seeds[i].Len2()))
			}
		}
		want = orderPolygon(want)
		if len(cell) != len(want) {
			t.Fatalf("cell %d has %d vertices, want %d", i, len(cell), len(want))
		}
		for j, p := range cell {
			if p.Sub(want[j]).Len() > 1e-9 {
				t.Fatalf("cell %d vertex %d: %v, want %v", i, j, p, want[j])
			}
		}
		area += faceArea(cell)
	}
	if math.Abs(area-6) > 1e-9 {
		t.Fatalf("extended background area %v, want 6", area)
	}
}

func TestAmbientHorizontalFade(t *testing.T) {
	for _, tc := range []struct{ x, alpha float64 }{
		{-1, 0}, {-.5, 0}, {-.25, .5}, {0, 1}, {.5, 1}, {1, 1}, {1.25, .5}, {1.5, 0}, {2, 0},
	} {
		if got := horizontalFade(tc.x); math.Abs(got-tc.alpha) > 1e-12 {
			t.Fatalf("fade at %v: %v, want %v", tc.x, got, tc.alpha)
		}
	}
}
