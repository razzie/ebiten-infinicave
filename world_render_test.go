package infinicave

import (
	"context"
	"image"
	"image/color"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
)

// Pixel readback needs a running Ebitengine loop. Use a child process so its
// one-shot loop cannot shut down the graphics driver used by other tests.
func TestRenderPassCompositing(t *testing.T) {
	if os.Getenv("INFINICAVE_COMPOSITOR_TEST") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRenderPassCompositing$", "-test.count=1")
		cmd.Env = append(os.Environ(), "INFINICAVE_COMPOSITOR_TEST=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("compositor pixel check: %v\n%s", err, output)
		}
		return
	}
	g := &compositorTestGame{t: t}
	ebiten.SetWindowSize(64, 64)
	if err := ebiten.RunGame(g); err != nil {
		t.Fatal(err)
	}
}

type compositorTestGame struct {
	t    *testing.T
	done bool
}

func (g *compositorTestGame) Layout(int, int) (int, int) { return 64, 64 }
func (g *compositorTestGame) Update() error {
	if g.done {
		return ebiten.Termination
	}
	return nil
}
func (g *compositorTestGame) Draw(*ebiten.Image) {
	if g.done {
		return
	}
	g.done = true
	red := color.RGBA{R: 200, A: 255}
	green := color.RGBA{G: 200, A: 255}
	blue := color.RGBA{B: 200, A: 255}
	orange := color.RGBA{R: 200, G: 100, A: 255}
	purple := color.RGBA{R: 180, B: 180, A: 255}
	layer := func(rect image.Rectangle, clr color.RGBA) *ebiten.Image {
		img := ebiten.NewImage(64, 64)
		img.SubImage(rect).(*ebiten.Image).Fill(clr)
		return img
	}
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		s := &worldSection{
			terrain:    ebiten.NewImage(128, 64),
			foreground: layer(image.Rect(4, 0, 12, 64), red), pixels: 64,
			grass:           layer(image.Rect(14, 8, 32, 24), green),
			foregroundVines: layer(image.Rect(34, 8, 42, 24), orange),
			mushrooms:       layer(image.Rect(44, 8, 62, 24), green),
			spiderWebs:      layer(image.Rect(4, 32, 22, 48), orange),
		}
		// Represent the ground patch separately from the blades in the same cache.
		s.grass.SubImage(image.Rect(24, 24, 32, 32)).(*ebiten.Image).Fill(orange)
		bounds := image.Rect(0, 64, 64, 128)
		s.foregroundVinesBounds, s.mushroomsBounds = bounds, bounds
		s.spiderWebsBounds = bounds
		scene := &Scene{orientation: orientation, world: &world{sections: map[int64]*worldSection{0: s}}}
		background, err := render.NewBackgroundRenderer(render.BackgroundConfig{BackgroundBlur: .002, ShadowOpacity: .5, ShadowBlur: .01, ShadowOffset: V{X: .02, Y: .02}})
		if err != nil {
			g.t.Error(err)
			return
		}
		scene.background = background
		dst := ebiten.NewImage(64, 64)
		viewport := Viewport{Y: -1, Height: 1, X: 0, Width: 1}
		check := func(img *ebiten.Image, x, y int, want color.RGBA) {
			got := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			if got != want {
				g.t.Errorf("%s pixel %d,%d: got %v, want %v", orientation, x, y, got, want)
			}
		}
		backCalls, frontCalls := 0, 0
		passes := RenderPasses{
			BehindRocks: func(c RenderContext) { backCalls++; c.Dst.Fill(blue) },
			AboveRocks: func(c RenderContext) {
				frontCalls++
				for _, sample := range []struct {
					x, y int
					clr  color.RGBA
				}{
					{8, 16, red}, {18, 16, blue}, {28, 16, blue}, {38, 16, orange}, {48, 16, green}, {58, 16, green},
					{8, 40, orange}, {18, 40, orange}, {28, 28, blue},
				} {
					check(c.Dst, sample.x, sample.y, sample.clr)
				}
				c.Dst.Fill(purple)
			},
		}
		scene.DrawWithPasses(dst, viewport, passes)
		for _, sample := range []struct {
			x, y int
			clr  color.RGBA
		}{
			{8, 16, purple}, {18, 16, green}, {28, 16, green}, {38, 16, purple}, {48, 16, purple}, {58, 16, purple},
			{8, 40, purple}, {18, 40, purple}, {28, 28, orange},
		} {
			x, y := sample.x, sample.y
			if orientation == Horizontal {
				x, y = 63-y, x
			}
			check(dst, x, y, sample.clr)
		}
		if backCalls != 1 || frontCalls != 1 {
			g.t.Error("passes were invoked more than once")
		}
		// Empty passes must produce exactly the same image as the existing API.
		plain, empty := ebiten.NewImage(64, 64), ebiten.NewImage(64, 64)
		scene.Draw(plain, viewport)
		scene.DrawWithPasses(empty, viewport, RenderPasses{})
		p, q := make([]byte, 64*64*4), make([]byte, 64*64*4)
		plain.ReadPixels(p)
		empty.ReadPixels(q)
		for i := range p {
			if p[i] != q[i] {
				g.t.Error("Draw and empty DrawWithPasses differ")
				break
			}
		}
		plain.Deallocate()
		empty.Deallocate()
		// Grass stays in the shadow silhouette even after being separated.
		mask := ebiten.NewImage(64, 64)
		scene.world.DrawForegroundSilhouette(mask, -1, 1, 0, render.TargetTransform(mask))
		check(mask, 18, 16, green)
		check(mask, 28, 16, green)
		check(mask, 28, 28, orange)
		mask.Deallocate()
		// Disabling backgrounds must bypass both direct and effect composition,
		// preserve the caller's pixels, and keep the foreground in both orientations.
		s.terrain.Fill(blue)
		s.vines = layer(image.Rect(48, 48, 56, 56), green)
		s.vinesBounds = bounds
		for _, effects := range []*render.BackgroundRenderer{nil, background} {
			scene.background = effects
			for _, disabled := range []bool{false, true} {
				scene.world.disableBackground = disabled
				dst.Fill(purple)
				scene.DrawWithPasses(dst, viewport, RenderPasses{
					BehindRocks: func(c RenderContext) {
						if disabled {
							want := purple
							if orientation == Horizontal {
								want = color.RGBA{}
							}
							check(c.Dst, 50, 50, want)
							check(c.Dst, 60, 60, want)
							return
						}
						check(c.Dst, 60, 60, blue)
					},
				})
				x, y := 8, 16
				if orientation == Horizontal {
					x, y = 63-y, x
				}
				check(dst, x, y, red)
			}
		}
		scene.world.disableBackground = false
		scene.background = background
		// Force an arrival, then enlarge it so at least one opaque pixel is
		// observable. Compare with a separate bat-only reference image.
		scene.bats = render.NewBatFlock(42, 60, orientation)
		scene.bats.Step(Viewport{Y: -1, Height: 1}, 1)
		for i := range scene.bats.Bats {
			scene.bats.Bats[i].Size = .15
			scene.bats.Bats[i].Start.X, scene.bats.Bats[i].End.X = .5, .5
		}
		batImage := ebiten.NewImage(64, 64)
		scene.bats.Draw(batImage, Viewport{Y: -1, Height: 1})
		if orientation == Horizontal {
			rotated := ebiten.NewImage(64, 64)
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Rotate(1.5707963267948966)
			op.GeoM.Translate(64, 0)
			rotated.DrawImage(batImage, op)
			batImage.Deallocate()
			batImage = rotated
		}
		scene.DrawWithPasses(dst, viewport, RenderPasses{AboveRocks: func(c RenderContext) { c.Dst.Fill(purple) }})
		found := false
		for y := 0; y < 64 && !found; y++ {
			for x := 0; x < 64; x++ {
				clr := color.RGBAModel.Convert(batImage.At(x, y)).(color.RGBA)
				if clr.A == 255 {
					check(dst, x, y, clr)
					found = true
					break
				}
			}
		}
		if !found {
			g.t.Error("bat fixture had no opaque pixel")
		}
		batImage.Deallocate()
		dst.Deallocate()
		s.deallocate()
		background.Close()
		if scene.orientedTarget != nil {
			scene.orientedTarget.Deallocate()
		}
	}
}
