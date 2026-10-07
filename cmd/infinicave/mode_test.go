package main

import (
	"math"
	"testing"
	"time"

	infinicave "github.com/razzie/ebiten-infinicave"
)

func TestViewerModesAndWindowSizing(t *testing.T) {
	for _, tc := range []struct {
		name          string
		mode          infinicave.Orientation
		width, height int
	}{
		{"portrait", infinicave.Vertical, 486, 864},
		{"landscape", infinicave.Horizontal, 1536, 864},
	} {
		mode, err := parseMode(tc.name)
		if err != nil || mode != tc.mode {
			t.Fatalf("mode %q: %v / %v", tc.name, mode, err)
		}
		w, h := windowSize(mode, 1920, 1080)
		if w != tc.width || h != tc.height {
			t.Fatalf("mode %q window: %d x %d", tc.name, w, h)
		}
	}
	for _, name := range []string{"", "horizontal", "vertical", "Landscape", "square"} {
		if _, err := parseMode(name); err == nil {
			t.Fatalf("accepted invalid mode %q", name)
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

func TestLandscapeHoverUsesWorldCoordinates(t *testing.T) {
	scene, err := infinicave.NewScene(infinicave.Config{
		Seed: 42, Orientation: infinicave.Horizontal, View: infinicave.ViewClay,
		LoadSection: func(id int64) infinicave.SectionContent {
			if id != 0 {
				return infinicave.SectionContent{}
			}
			return infinicave.SectionContent{Guides: []infinicave.Guide{{Pts: []infinicave.V{{X: .2, Y: .5}, {X: .8, Y: .5}}}}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(scene.Close)
	scene.SetRenderWidth(100)
	viewport := infinicave.Viewport{X: .00049, Width: 1.8}
	deadline := time.Now().Add(20 * time.Second)
	for !scene.Update(viewport) {
		if time.Now().After(deadline) {
			t.Fatal("landscape hover fixture did not load")
		}
		time.Sleep(time.Millisecond)
	}
	target := hoverAt(scene, infinicave.V{X: .5, Y: .5}, viewport, 100)
	if target.kind != infinicave.TargetGuide {
		t.Fatalf("landscape guide hover disagrees with the raster origin: %+v", target)
	}
	r, err := newHoverRenderer()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.close)
	r.selectTarget(target, scene, 100, 0, 1)
	if r.image == nil || r.image.Bounds().Dx() < r.image.Bounds().Dy() || r.orientation != infinicave.Horizontal {
		t.Fatal("landscape guide highlight has incorrect world bounds")
	}
	for _, cursor := range []infinicave.V{{X: -.001, Y: .5}, {X: 1.8, Y: .5}, {X: .5, Y: -.001}, {X: .5, Y: 1}} {
		if got := hoverAt(scene, cursor, viewport, 100); got.kind != 0 {
			t.Fatalf("landscape margin selected terrain: %v", cursor)
		}
	}
}
