package main

import (
	"math"
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

func TestLandscapeCameraCoordinatesAndResize(t *testing.T) {
	for _, size := range [][2]int{{1600, 900}, {501, 800}, {1001, 600}, {0, 0}} {
		g := &Game{mode: infinicave.Horizontal}
		w, h := g.Layout(size[0], size[1])
		pixels := min(w, h)
		wantExtent := float64(w) / float64(pixels)
		v := g.viewport()
		if v.X != 0 || v.Width != wantExtent || g.renderOffsetX() != 0 || g.renderOffsetY() != float64(h-pixels)/2 {
			t.Fatalf("horizontal layout %v: %+v", size, v)
		}
		g.camera.Y -= 3.123456
		g.camera.Velocity = -.01
		v = g.viewport()
		if math.Abs(v.X-3.123456) > 1e-12 || v.Velocity != .01 {
			t.Fatalf("horizontal camera movement: %+v", v)
		}
		cursor := infinicave.V{X: float64(pixels) * .6, Y: g.renderOffsetY() + float64(pixels)*.4}
		world := g.worldPoint(cursor)
		if math.Abs(world.Y-.4) > 1e-12 || math.Abs(world.X-(g.cameraX()+.6)) > 1e-12 {
			t.Fatalf("horizontal cursor: %v", world)
		}
		x, y := g.screenPoint(world)
		if math.Abs(float64(x)-cursor.X) > .0001 || math.Abs(float64(y)-cursor.Y) > .0001 {
			t.Fatal("horizontal carve preview does not match cursor coordinates")
		}
		if g.worldPoint(infinicave.V{Y: g.renderOffsetY() - 1}).Y >= 0 ||
			g.worldPoint(infinicave.V{Y: g.renderOffsetY() + float64(pixels) + 1}).Y <= 1 {
			t.Fatal("horizontal margins map inside the cave")
		}
		before := g.viewport().X
		g.Layout(w*2, h*2)
		if math.Abs(g.viewport().X-before) > 1e-12 {
			t.Fatal("uniform resize moved the landscape camera")
		}
		g.camera.glideTo(g.camera.floor())
		for i := 0; i < 400; i++ {
			g.camera.step()
		}
		if g.viewport().X != 0 || g.camera.Velocity != 0 {
			t.Fatal("horizontal camera did not return to the starting edge")
		}
	}
}

func TestLandscapeExportLayout(t *testing.T) {
	g := &Game{mode: infinicave.Horizontal, output: "scene.png"}
	for _, size := range [][2]int{{1600, 900}, {500, 800}, {800, 500}} {
		w, h := g.Layout(size[0], size[1])
		v := g.viewport()
		if w != exportHeight || h != renderWidth || v.X != 0 || v.Width != 2.4 {
			t.Fatalf("horizontal export changed: %d x %d / %+v", w, h, v)
		}
	}
}
