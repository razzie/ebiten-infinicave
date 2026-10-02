package main

import "testing"

func TestCameraGrowsUpwardWithoutLimit(t *testing.T) {
	g := &Game{}
	w, h := g.Layout(1000, 800)
	if w != W || h != 800 || g.camera.Y != -800 || g.camera.Target != -800 {
		t.Fatalf("must start at the floor: %+v", g.camera)
	}
	g.camera.push(-10)
	prev := g.camera.Y
	for i := 0; i < 2000; i++ {
		g.camera.step()
		if g.camera.Y > prev {
			t.Fatal("camera moved backwards")
		}
		prev = g.camera.Y
	}
	if g.camera.Velocity != 0 || g.camera.Y > -800-100 {
		t.Fatalf("momentum did not coast to rest: %+v", g.camera)
	}
	g.camera.push(cameraMaxSpeed * 10)
	if g.camera.Velocity != cameraMaxSpeed {
		t.Fatal("speed is not capped")
	}
	for i := 0; i < 100; i++ {
		g.camera.step()
	}
	if g.camera.Y != -800 || g.camera.Velocity != 0 {
		t.Fatal("camera passed below starting floor")
	}
}

func TestCameraGlideAndMoveWithoutSections(t *testing.T) {
	g := &Game{world: &World{sections: map[int64]*worldSection{}, jobs: make(chan int64, 1)}}
	g.Layout(1000, 800)
	g.camera.Y = -50000
	g.camera.glideTo(-800)
	for i := 0; i < 400 && g.camera.Y != -800; i++ {
		g.camera.step()
	}
	if g.camera.Y != -800 {
		t.Fatal("glide did not arrive")
	}
	g.camera.push(-20)
	g.camera.step()
	if g.world.ensure(g.camera.Y, g.camera.Height, g.camera.Velocity) || g.camera.Y >= -800 {
		t.Fatal("camera must keep moving while sections are missing")
	}
}

func TestInfiniteCameraResize(t *testing.T) {
	g := &Game{}
	g.Layout(1000, 800)
	g.camera.Y, g.camera.Target = -5300, -5300
	w, h := g.Layout(500, 400)
	if w != W || h != 800 || g.camera.Y != -5300 || g.camera.Target != -5300 {
		t.Fatal("resize reset position")
	}
	g.camera.Y, g.camera.Target = -800, -800
	g.Layout(1000, 1200)
	if g.camera.Y != -1200 || g.camera.Target != -1200 {
		t.Fatal("resize exposed space below the floor")
	}
}

func TestVisibleSectionsAtSeams(t *testing.T) {
	for _, tc := range []struct {
		y         float64
		height    int
		low, high int64
	}{
		{-800, 800, 0, 0}, {-1000, 1000, 0, 0}, {-1001, 800, 0, 1}, {-2000, 1000, 1, 1}, {-10800, 800, 10, 10}, {-1e9, 800, 999999, 999999},
	} {
		low, high := visibleSections(tc.y, tc.height)
		if low != tc.low || high != tc.high {
			t.Fatalf("viewport %v/%d: sections %d..%d, want %d..%d", tc.y, tc.height, low, high, tc.low, tc.high)
		}
	}
}
