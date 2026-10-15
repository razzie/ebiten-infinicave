package infinicave

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
)

// Run in a fresh process so atlas construction and shader compilation are cold.
// Timing is diagnostic: machine speed must not make this test flaky.
func TestColdSceneStartup(t *testing.T) {
	if os.Getenv("INFINICAVE_STARTUP") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestColdSceneStartup$", "-test.v")
		cmd.Env = append(os.Environ(), "INFINICAVE_STARTUP=1")
		out, err := cmd.CombinedOutput()
		t.Logf("%s", out)
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	g := &startupGame{t: t, start: time.Now()}
	config := DefaultConfig()
	config.Seed = 42
	config.OnCollisionReady = func(CollisionGeometry) { g.t.Logf("collision: %s", time.Since(g.start)) }
	var err error
	g.scene, err = NewScene(config)
	if err != nil {
		t.Fatal(err)
	}
	defer g.scene.Close()
	ebiten.SetWindowSize(640, 640)
	ebiten.SetRunnableOnUnfocused(true)
	g.scene.SetRenderWidth(640)
	g.scene.Update(Viewport{Y: -1, Height: 1})
	if err := ebiten.RunGame(g); err != nil {
		t.Fatal(err)
	}
}

type startupGame struct {
	t                       *testing.T
	scene                   *Scene
	start                   time.Time
	ready, foreground, done bool
	frames                  int
	products                [vegetationProducts]bool
}

func (g *startupGame) Layout(int, int) (int, int) { return 640, 640 }
func (g *startupGame) Update() error {
	if g.done {
		return ebiten.Termination
	}
	if time.Since(g.start) > 30*time.Second {
		g.t.Fatalf("startup stalled: frames=%d, sections=%d pending=%d inflight=%d upload=%+v", g.frames, len(g.scene.world.sections), len(g.scene.world.pending), len(g.scene.world.inflight), g.scene.world.upload != nil)
	}
	g.scene.SetRenderWidth(640)
	g.ready = g.scene.Update(Viewport{Y: -1, Height: 1})
	return nil
}
func (g *startupGame) Draw(screen *ebiten.Image) {
	if g.done {
		return
	}
	g.frames++
	g.scene.Draw(screen, Viewport{Y: -1, Height: 1})
	if g.frames == 1 {
		screen.At(0, 0)
		g.t.Logf("first completed frame: %s", time.Since(g.start))
	}
	s := g.scene.world.sections[0]
	if !g.foreground && s != nil && s.foreground != nil {
		screen.At(0, 0) // include GPU completion through this frame
		g.foreground = true
		g.t.Logf("first foreground: %s, frame %d", time.Since(g.start), g.frames)
	}
	if s != nil {
		flushed := false
		for kind, name := range []string{"background vines", "webs", "mushrooms", "foreground vines"} {
			if !g.products[kind] && s.plants[kind].pixels > 0 {
				if !flushed {
					screen.At(0, 0)
					flushed = true
				}
				g.products[kind] = true
				g.t.Logf("%s published: %s, frame %d", name, time.Since(g.start), g.frames)
			}
		}
	}
	if g.ready {
		screen.At(0, 0)
		g.t.Logf("all visible layers: %s, frame %d", time.Since(g.start), g.frames)
		g.done = true
	}
}
