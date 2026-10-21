package main

import (
	"testing"

	infinicave "github.com/razzie/ebiten-infinicave"
)

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
	g.setCarveStatus("previous cut")
	g.Layout(500, 400)
	if err := g.Update(); err != nil {
		t.Fatal(err)
	}
	if g.regenerate || g.seed != config.Seed || g.carving.active || g.carveStatus != "previous cut" || !g.loading {
		t.Fatal("resize must refresh rendering with the current seed and retain carving status")
	}
	// Further ticks at the same size must not reset fresh gesture/status state.
	g.setCarveStatus("new cut")
	g.Layout(500, 400)
	if err := g.Update(); err != nil {
		t.Fatal(err)
	}
	if g.carveStatus != "new cut" {
		t.Fatal("unchanged layout regenerated again")
	}
}

func TestMouseFogVelocity(t *testing.T) {
	for _, mode := range []infinicave.Orientation{infinicave.Vertical, infinicave.Horizontal} {
		for _, size := range [][2]int{{800, 800}, {1600, 900}, {501, 801}, {1002, 1602}} {
			g := &Game{mode: mode}
			g.Layout(size[0], size[1])
			center := infinicave.V{X: float64(size[0]) / 2, Y: float64(size[1]) / 2}
			if got := g.mouseFogVelocity(center); got != (infinicave.V{}) {
				t.Fatalf("%v %v: center must stop fog, got %v", mode, size, got)
			}
			// Equal pixel offsets must give equal X/Y speeds, even on rectangular
			// windows, so wind points at the cursor in public world coordinates.
			distance := float64(min(size[0], size[1])) / 2
			for _, direction := range []infinicave.V{{X: 1}, {X: -1}, {Y: 1}, {Y: -1}, {X: 1, Y: -1}, {X: -.5, Y: .5}} {
				cursor := center.Add(direction.Mul(distance))
				got, want := g.mouseFogVelocity(cursor), direction.Mul(.1)
				if got.Sub(want).Len() > 1e-12 {
					t.Fatalf("%v %v: cursor %v velocity %v, want %v", mode, size, cursor, got, want)
				}
			}
		}
	}
	if got := (&Game{}).mouseFogVelocity(infinicave.V{X: 100}); got != (infinicave.V{}) {
		t.Fatal("missing layout must stop fog")
	}
}

func TestMouseFogDisabledAndExportSkipInput(t *testing.T) {
	// Without a Scene these calls also catch accidental velocity updates when
	// the flag is off or exporting; neither should read interactive input.
	for _, g := range []*Game{{}, {fogMouse: true, output: "scene.png"}} {
		g.updateFogVelocity()
	}
}
