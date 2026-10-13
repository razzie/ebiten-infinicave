package infinicave

import (
	"os"
	"sort"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Opt-in latency capture runs in a real game loop and waits for GPU completion.
// Run in a fresh process, including when comparing an archived release:
// INFINICAVE_CARVE_LATENCY=1 go test . -run '^TestCarveLatencyCapture$' -v
func TestCarveLatencyCapture(t *testing.T) {
	if os.Getenv("INFINICAVE_CARVE_LATENCY") != "1" {
		t.Skip("set INFINICAVE_CARVE_LATENCY=1 for GPU-completion measurements")
	}
	g := &carveLatencyGame{t: t}
	ebiten.SetWindowSize(256, 256)
	ebiten.SetRunnableOnUnfocused(true)
	if err := ebiten.RunGame(g); err != nil {
		t.Fatal(err)
	}
}

type carveLatencyGame struct {
	t    *testing.T
	done bool
}

func (g *carveLatencyGame) Layout(int, int) (int, int) { return 256, 256 }
func (g *carveLatencyGame) Update() error {
	if g.done {
		return ebiten.Termination
	}
	return nil
}
func (g *carveLatencyGame) Draw(*ebiten.Image) {
	if g.done {
		return
	}
	g.done = true
	config := DefaultConfig()
	config.Seed, config.Fog, config.BatsPerMinute = 42, 0, 0
	scene, err := NewScene(config)
	if err != nil {
		g.t.Fatal(err)
	}
	defer scene.Close()
	prepared := make([]render.SectionMesh, 3)
	for i := range prepared {
		data := terrain.BuildSection(42, int64(i))
		prepared[i] = render.PrepareSection(data, ViewShaded, render.RockGrowthOptions{Moss: 1, Grass: 1, Hanging: 1})
		prepared[i].Geometry = terrain.PrepareTerrainGeometry(data, 0)
		render.FinishSectionMesh(&prepared[i])
	}
	// Drawing every edited layer before readback includes its queued GPU work.
	target := ebiten.NewImage(768, 768)
	defer target.Deallocate()
	readback := make([]byte, 4*768*768)
	flush := func() {
		target.Clear()
		for id := int64(0); id < int64(len(prepared)); id++ {
			s := scene.world.sections[id]
			for _, layer := range []*ebiten.Image{s.foreground, s.grass, s.foregroundVines, s.mushrooms, s.spiderWebs} {
				if layer != nil {
					target.DrawImage(layer, nil)
				}
			}
		}
		target.ReadPixels(readback)
	}
	for _, radius := range []float64{.05, .2, .4} {
		for _, overlap := range []bool{false, true} {
			var submissions, completions []time.Duration
			for sample := 0; sample < 5; sample++ {
				scene.world.close()
				scene.world = &world{sections: make(map[int64]*worldSection), done: make(chan struct{}), pixels: 768}
				for _, mesh := range prepared {
					scene.world.startUpload(mesh)
					for ticks := 0; scene.world.upload != nil && ticks < 10000; ticks++ {
						scene.world.receive(scene)
					}
					if scene.world.upload != nil {
						g.t.Fatal("upload stalled")
					}
				}
				scene.world.queryIndex()
				if overlap {
					if _, err := scene.CarveCircle(V{X: .45, Y: -.5}, radius); err != nil {
						g.t.Fatal(err)
					}
				}
				flush()
				start := time.Now()
				if _, err := scene.CarveCircle(V{X: .5, Y: -.5}, radius); err != nil {
					g.t.Fatal(err)
				}
				submissions = append(submissions, time.Since(start))
				flush()
				completions = append(completions, time.Since(start))
			}
			sort.Slice(submissions, func(i, j int) bool { return submissions[i] < submissions[j] })
			sort.Slice(completions, func(i, j int) bool { return completions[i] < completions[j] })
			g.t.Logf("radius=%.2f overlap=%v: submission median=%v; completion median=%v max=%v", radius, overlap, submissions[2], completions[2], completions[4])
		}
	}
}
