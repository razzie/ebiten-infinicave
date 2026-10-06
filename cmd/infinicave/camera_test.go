package main

import (
	"testing"
)

func TestCameraGrowsUpwardWithoutLimit(t *testing.T) {
	g := &Game{}
	w, h := g.Layout(1000, 800)
	if w != renderWidth || h != 800 || g.camera.Y != -.8 || g.camera.Target != -.8 {
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
	if g.camera.Velocity != 0 || g.camera.Y > -.8-.1 {
		t.Fatalf("momentum did not coast to rest: %+v", g.camera)
	}
	g.camera.push(cameraMaxSpeed * 10)
	if g.camera.Velocity != cameraMaxSpeed {
		t.Fatal("speed is not capped")
	}
	for i := 0; i < 100; i++ {
		g.camera.step()
	}
	if g.camera.Y != -.8 || g.camera.Velocity != 0 {
		t.Fatal("camera passed below starting floor")
	}
}

func TestCameraGlideAndMoveWithoutSections(t *testing.T) {
	g := &Game{}
	g.Layout(1000, 800)
	g.camera.Y = -50
	g.camera.glideTo(-.8)
	for i := 0; i < 400 && g.camera.Y != -.8; i++ {
		g.camera.step()
	}
	if g.camera.Y != -.8 {
		t.Fatal("glide did not arrive")
	}
	g.camera.push(-.02)
	g.camera.step()
	if g.camera.Y >= -.8 {
		t.Fatal("camera must keep moving while sections are missing")
	}
}

func TestInfiniteCameraResize(t *testing.T) {
	g := &Game{}
	g.Layout(1000, 800)
	g.camera.Y, g.camera.Target = -5.3, -5.3
	w, h := g.Layout(500, 400)
	if w != renderWidth || h != 800 || g.camera.Y != -5.3 || g.camera.Target != -5.3 {
		t.Fatal("resize reset position")
	}
	g.camera.Y, g.camera.Target = -.8, -.8
	g.Layout(1000, 1200)
	if g.camera.Y != -1.2 || g.camera.Target != -1.2 {
		t.Fatal("resize exposed space below the floor")
	}
}
