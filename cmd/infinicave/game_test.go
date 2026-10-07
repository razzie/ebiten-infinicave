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
