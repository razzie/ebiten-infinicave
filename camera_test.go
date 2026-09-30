package main

import "testing"

func TestCameraGrowsUpwardWithoutLimit(t *testing.T) {
	g := &Game{}
	w, h := g.Layout(1000, 800)
	if w != W || h != 800 || g.camera.Y != -800 || g.camera.Target != -800 {
		t.Fatalf("must start at the floor: %+v", g.camera)
	}
	g.camera.scroll(-173.5)
	if g.camera.Target != -973.5 {
		t.Fatal("fractional scrolling lost")
	}
	g.camera.scroll(-1e9)
	if g.camera.Target != -1000000973.5 {
		t.Fatal("upward travel was capped")
	}
	g.camera.scroll(2e9)
	if g.camera.Target != -800 {
		t.Fatal("camera passed below starting floor")
	}
}

func TestInfiniteCameraResize(t *testing.T) {
	g := &Game{}
	g.Layout(1000, 800)
	g.camera.scroll(-4500)
	g.camera.Y = g.camera.Target
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
