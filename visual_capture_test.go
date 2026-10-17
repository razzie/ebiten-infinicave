package infinicave

import (
	"image/color"
	"image/png"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// An opt-in, fixed-camera export usable on both the old and optimized versions.
// Run each capture in a fresh test process (Ebitengine permits one game loop).
func TestVisualCapture(t *testing.T) {
	path := os.Getenv("INFINICAVE_CAPTURE")
	if path == "" {
		t.Skip("set INFINICAVE_CAPTURE to a PNG path")
	}
	config := DefaultConfig()
	config.Seed = 42
	if seed, err := strconv.ParseInt(os.Getenv("INFINICAVE_CAPTURE_SEED"), 10, 64); err == nil {
		config.Seed = seed
	}
	config.Fog, config.BatsPerMinute = 0, 0
	config.AppearanceFade = false
	if os.Getenv("INFINICAVE_CAPTURE_EDGES") == "0" {
		config.SpikeDensity, config.EdgeRoughness = 0, 0
	}
	if os.Getenv("INFINICAVE_CAPTURE_FOG") == "1" {
		config.Fog = DefaultConfig().Fog
	}
	if strategy := os.Getenv("INFINICAVE_CAPTURE_BRANCHING"); strategy != "" {
		var err error
		config.BranchingStrategy, err = ParseBranchingStrategy(strategy)
		if err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("INFINICAVE_CAPTURE_MODE") == "landscape" {
		config.Orientation = Horizontal
	}
	if os.Getenv("INFINICAVE_CAPTURE_LAYER") == "grass" {
		config.MossDensity, config.HangingGrowthDensity, config.MushroomDensity, config.SpiderWebDensity = 0, 0, 0, 0
		config.GrassDensity = 1
	}
	width := 640
	if pixels, err := strconv.Atoi(os.Getenv("INFINICAVE_CAPTURE_WIDTH")); err == nil && pixels > 0 {
		width = pixels
	}
	scene, err := NewScene(config)
	if err != nil {
		t.Fatal(err)
	}
	defer scene.Close()
	g := &visualCaptureGame{t: t, scene: scene, path: path, width: width, start: time.Now()}
	g.viewport = Viewport{Y: -2.4, Height: 2.4}
	g.w, g.h = width, width*12/5
	if config.Orientation == Horizontal {
		g.viewport = Viewport{X: 0, Width: 2.4}
		g.w, g.h = g.h, g.w
	}
	ebiten.SetWindowSize(640, 480)
	ebiten.SetRunnableOnUnfocused(true)
	scene.SetRenderWidth(width)
	if err := ebiten.RunGame(g); err != nil {
		t.Fatal(err)
	}
}

type visualCaptureGame struct {
	t           *testing.T
	scene       *Scene
	viewport    Viewport
	path        string
	width, w, h int
	ready, done bool
	start       time.Time
}

func (g *visualCaptureGame) Layout(int, int) (int, int) { return g.w, g.h }
func (g *visualCaptureGame) Update() error {
	if g.done {
		return ebiten.Termination
	}
	if time.Since(g.start) > time.Minute {
		g.t.Fatal("capture generation timed out")
	}
	g.ready = g.scene.Update(g.viewport)
	return nil
}
func (g *visualCaptureGame) Draw(screen *ebiten.Image) {
	if g.done {
		return
	}
	passes := RenderPasses{}
	if os.Getenv("INFINICAVE_CAPTURE_LAYER") == "grass" {
		passes.AboveRocks = func(c RenderContext) {
			c.Dst.Fill(color.NRGBA{R: 25, G: 29, B: 30, A: 255})
			for id, s := range g.scene.world.sections {
				if s.geometry == nil {
					continue
				}
				for _, cell := range s.geometry.Topology.Grid {
					for j, a := range cell.Polygon {
						b := cell.Polygon[(j+1)%len(cell.Polygon)]
						a = terrain.WorldPoint(terrain.Orientation(c.Orientation), a.Add(V{Y: terrain.SectionTop(id)}))
						b = terrain.WorldPoint(terrain.Orientation(c.Orientation), b.Add(V{Y: terrain.SectionTop(id)}))
						x0, y0 := c.WorldToTarget.Apply(a.X, a.Y)
						x1, y1 := c.WorldToTarget.Apply(b.X, b.Y)
						vector.StrokeLine(c.Dst, float32(x0), float32(y0), float32(x1), float32(y1), .6, color.NRGBA{R: 70, G: 100, B: 115, A: 180}, true)
					}
				}
			}
		}
	}
	if g.scene.fog != nil {
		g.scene.fog.Time = 0
	}
	g.scene.DrawWithPasses(screen, g.viewport, passes)
	if os.Getenv("INFINICAVE_CAPTURE_LAYER") == "grass" {
		viewport, _ := orientedViewport(g.scene.orientation, g.viewport)
		project := func(p V) (float32, float32) {
			x := p.X * float64(g.width)
			y := (p.Y - viewport.Y) * float64(g.width)
			if g.scene.orientation == Horizontal {
				x, y = float64(g.w)-y, x
			}
			return float32(x), float32(y)
		}
		for id, s := range g.scene.world.sections {
			if s.mesh == nil {
				continue
			}
			for _, a := range s.mesh.Foreground.Growth {
				if a.Kind != render.GrassAttachment || a.Mesh.Texture != render.NoAtlas || a.Dead {
					continue
				}
				for j, p := range a.Anchors {
					if p.Y < 0 || p.Y >= 1 {
						continue
					}
					x, y := project(p.Add(V{Y: terrain.SectionTop(id)}))
					vector.FillCircle(screen, x, y, 1.2, color.NRGBA{R: 255, G: 130, B: 100, A: 255}, true)
					if j > 0 && p.Sub(a.Anchors[j-1]).Len() < .010 {
						x0, y0 := project(a.Anchors[j-1].Add(V{Y: terrain.SectionTop(id)}))
						vector.StrokeLine(screen, x0, y0, x, y, .6, color.NRGBA{R: 100, G: 210, B: 150, A: 255}, true)
					}
				}
			}
		}
	}
	if !g.ready {
		return
	}
	f, err := os.Create(g.path)
	if err != nil {
		g.t.Fatal(err)
	}
	err = png.Encode(f, screen)
	closeErr := f.Close()
	if err != nil {
		g.t.Fatal(err)
	}
	if closeErr != nil {
		g.t.Fatal(closeErr)
	}
	g.done = true
}
