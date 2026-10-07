package main

import (
	"testing"

	infinicave "github.com/razzie/ebiten-infinicave"
)

func TestCameraGrowsUpwardWithoutLimit(t *testing.T) {
	g := &Game{}
	w, h := g.Layout(1000, 800)
	if w != renderWidth || h != 800 || g.camera.Y != -1 || g.camera.Target != -1 {
		t.Fatalf("must start at the floor: %+v", g.camera)
	}
	g.camera.push(-.01)
	prev := g.camera.Y
	for i := 0; i < 2000; i++ {
		g.camera.step()
		if g.camera.Y > prev {
			t.Fatal("camera moved backwards")
		}
		prev = g.camera.Y
	}
	if g.camera.Velocity != 0 || g.camera.Y > -1-.1 {
		t.Fatalf("momentum did not coast to rest: %+v", g.camera)
	}
	g.camera.push(cameraMaxSpeed * 10)
	if g.camera.Velocity != cameraMaxSpeed {
		t.Fatal("speed is not capped")
	}
	for i := 0; i < 100; i++ {
		g.camera.step()
	}
	if g.camera.Y != -1 || g.camera.Velocity != 0 {
		t.Fatal("camera passed below starting floor")
	}
}

func TestCameraGlideAndMoveWithoutSections(t *testing.T) {
	g := &Game{}
	g.Layout(1000, 800)
	g.camera.Y = -50
	g.camera.glideTo(-1)
	for i := 0; i < 400 && g.camera.Y != -1; i++ {
		g.camera.step()
	}
	if g.camera.Y != -1 {
		t.Fatal("glide did not arrive")
	}
	g.camera.push(-.02)
	g.camera.step()
	if g.camera.Y >= -1 {
		t.Fatal("camera must keep moving while sections are missing")
	}
}

func TestInfiniteCameraResize(t *testing.T) {
	g := &Game{}
	g.Layout(1000, 800)
	g.camera.Y, g.camera.Target = -5.3, -5.3
	w, h := g.Layout(500, 400)
	if w != 500 || h != 400 || g.camera.Y != -5.3 || g.camera.Target != -5.3 {
		t.Fatal("resize must render at window dimensions and keep position")
	}
	g.camera.Y, g.camera.Target = -1, -1
	g.Layout(1000, 1200)
	if g.camera.Y != -1.2 || g.camera.Target != -1.2 {
		t.Fatal("resize exposed space below the floor")
	}
}

func TestLayoutUpdatesResolutionWithoutResettingWorld(t *testing.T) {
	g := &Game{}
	g.Layout(1000, 800)
	if g.regenerate {
		t.Fatal("initial layout should use the existing scene")
	}
	g.Layout(1000, 800)
	if g.regenerate {
		t.Fatal("unchanged layout queued regeneration")
	}
	for _, size := range [][2]int{{500, 400}, {501, 400}, {501, 401}} {
		g.regenerate = false
		w, h := g.Layout(size[0], size[1])
		if w != size[0] || h != size[1] || g.regenerate {
			t.Fatalf("resize to %v did not update resolution without resetting the world", size)
		}
		g.regenerate = false
		g.Layout(size[0], size[1])
		if g.regenerate {
			t.Fatal("unchanged layout queued regeneration")
		}
	}
}

func TestLayoutClampsEmptyWindowDimensions(t *testing.T) {
	g := &Game{}
	w, h := g.Layout(0, 0)
	if w != 1 || h != 1 || g.camera.Height != 1 {
		t.Fatalf("invalid layout: %d x %d, camera %+v", w, h, g.camera)
	}
}

func TestResizeUpdatePreservesSeedAndCarvingStatus(t *testing.T) {
	config := infinicave.DefaultConfig()
	config.Seed = 42
	scene, err := infinicave.NewScene(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(scene.Close)
	g := &Game{scene: scene, seed: config.Seed}
	g.Layout(1000, 800)
	g.carving = carveGesture{active: true}
	g.carveStatus = "previous cut"
	g.Layout(500, 400)
	if err := g.Update(); err != nil {
		t.Fatal(err)
	}
	if g.regenerate || g.seed != config.Seed || g.carving.active || g.carveStatus != "previous cut" || !g.loading {
		t.Fatal("resize must refresh rendering with the current seed and retain carving status")
	}
	// Further ticks at the same size must not reset fresh gesture/status state.
	g.carveStatus = "new cut"
	g.Layout(500, 400)
	if err := g.Update(); err != nil {
		t.Fatal(err)
	}
	if g.carveStatus != "new cut" {
		t.Fatal("unchanged layout regenerated again")
	}
}

func TestExportLayoutRemainsFixedOnWindowResize(t *testing.T) {
	g := &Game{output: "scene.png"}
	for _, size := range [][2]int{{1000, 800}, {500, 400}, {1200, 900}} {
		w, h := g.Layout(size[0], size[1])
		if w != renderWidth || h != exportHeight || g.regenerate || g.camera.Height != 2.4 || g.camera.Y != -2.4 {
			t.Fatalf("window resize changed export layout: %d x %d, camera %+v", w, h, g.camera)
		}
	}
}

func TestNativeLayoutIncludesMonitorScale(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 2} {
		g := &Game{}
		w, h := g.layoutNative(1000, 800, scale)
		if w != 1000*scale || h != 800*scale || g.camera.Height != 1 {
			t.Fatalf("scale %v: screen %v x %v, camera height %v", scale, w, h, g.camera.Height)
		}
	}
}

func TestLandscapeAndPortraitCoordinates(t *testing.T) {
	for _, tc := range []struct {
		width, height, pixels int
		offset, sceneHeight   float64
	}{
		{1600, 800, 800, 400, 1},
		{1001, 800, 800, 100.5, 1},
		{800, 800, 800, 0, 1},
		{500, 800, 500, 0, 1.6},
	} {
		g := &Game{}
		g.Layout(tc.width, tc.height)
		if g.renderPixels() != tc.pixels || g.renderOffsetX() != tc.offset || g.camera.Height != tc.sceneHeight {
			t.Fatalf("layout %dx%d: pixels %d, offset %v, height %v", tc.width, tc.height, g.renderPixels(), g.renderOffsetX(), g.camera.Height)
		}
		cursor := infinicave.V{X: tc.offset + float64(tc.pixels)/2, Y: float64(tc.pixels) / 2}
		got := g.worldPoint(cursor)
		want := infinicave.V{X: .5, Y: .5 - tc.sceneHeight}
		if got.Sub(want).Len() > 1e-12 {
			t.Fatalf("center cursor %v, want %v", got, want)
		}
		if g.worldPoint(infinicave.V{X: tc.offset - 1}).X >= 0 {
			t.Fatal("left margin mapped inside cave")
		}
		if g.worldPoint(infinicave.V{X: tc.offset + float64(tc.pixels) + 1}).X <= 1 {
			t.Fatal("right margin mapped inside cave")
		}
	}
}
