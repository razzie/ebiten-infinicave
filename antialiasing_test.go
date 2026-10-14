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
	"github.com/hajimehoshi/ebiten/v2/vector"
)

func TestAntialiasingConfiguration(t *testing.T) {
	if DefaultConfig().Antialiasing != AntialiasingFull || (Config{}).Antialiasing != AntialiasingFull {
		t.Fatal("default geometry antialiasing changed")
	}
	for _, mode := range []AntialiasingMode{AntialiasingFull, AntialiasingOff, AntialiasingFXAA} {
		parsed, err := ParseAntialiasingMode(mode.String())
		if err != nil || parsed != mode {
			t.Fatalf("mode round trip: %v / %v", parsed, err)
		}
		scene, err := NewScene(Config{Antialiasing: mode})
		if err != nil {
			t.Fatal(err)
		}
		fxaa := scene.fxaa
		if (fxaa != nil) != (mode == AntialiasingFXAA) {
			t.Fatal("postprocessing allocated for incorrect mode")
		}
		scene.Reset(42)
		if scene.world.antialiasing != mode || scene.fxaa != fxaa {
			t.Fatal("Reset lost antialiasing policy or postprocessing resources")
		}
		scene.Close()
		scene.Close()
	}
	if _, err := ParseAntialiasingMode("invalid"); err == nil {
		t.Fatal("invalid antialiasing flag accepted")
	}
	if _, err := NewScene(Config{Antialiasing: 255}); err == nil {
		t.Fatal("invalid antialiasing enum accepted")
	}
}

// A fresh child process isolates Ebitengine's one permitted game loop.
func TestAntialiasingPixels(t *testing.T) {
	if os.Getenv("INFINICAVE_AA_PIXELS") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAntialiasingPixels$", "-test.count=1")
		cmd.Env = append(os.Environ(), "INFINICAVE_AA_PIXELS=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("antialiasing pixels: %v\n%s", err, out)
		}
		return
	}
	ebiten.SetWindowSize(80, 80)
	if err := ebiten.RunGame(&antialiasingPixelGame{t: t}); err != nil {
		t.Fatal(err)
	}
}

type antialiasingPixelGame struct {
	t    *testing.T
	done bool
}

func (*antialiasingPixelGame) Layout(int, int) (int, int) { return 80, 80 }
func (g *antialiasingPixelGame) Update() error {
	if g.done {
		return ebiten.Termination
	}
	return nil
}
func (g *antialiasingPixelGame) Draw(*ebiten.Image) {
	if g.done {
		return
	}
	g.done = true
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		for _, mode := range []AntialiasingMode{AntialiasingFull, AntialiasingOff, AntialiasingFXAA} {
			scene, err := NewScene(Config{Antialiasing: mode, Orientation: orientation, DisableBackground: true})
			if err != nil {
				g.t.Fatal(err)
			}
			for _, size := range []image.Point{{64, 49}, {71, 57}} {
				dst := ebiten.NewImage(size.X, size.Y)
				viewport := Viewport{Y: -2, Height: 2}
				if orientation == Horizontal {
					viewport = Viewport{X: 0, Width: 2}
				}
				calls := 0
				paint := func(c RenderContext) {
					calls++
					if c.Antialiasing != mode {
						g.t.Fatal("hook lost antialiasing policy")
					}
					var path vector.Path
					path.MoveTo(10.25, 8.25)
					path.LineTo(40.25, 8.25)
					path.LineTo(10.25, 35.25)
					path.Close()
					vector.FillPath(c.Dst, &path, nil, &vector.DrawPathOptions{AntiAlias: c.Antialiasing.GeometryAA()})
				}
				scene.DrawWithPasses(dst, viewport, RenderPasses{AboveRocks: paint})
				if calls != 1 {
					g.t.Fatal("postprocessing repeated render hook")
				}
				pixels := make([]byte, 4*size.X*size.Y)
				dst.ReadPixels(pixels)
				fractional := 0
				for i := 0; i < len(pixels); i += 4 {
					a := pixels[i+3]
					if a > 0 && a < 255 {
						fractional++
					}
					if pixels[i] > a || pixels[i+1] > a || pixels[i+2] > a {
						g.t.Fatal("filter violated premultiplied alpha")
					}
				}
				if (fractional > 0) != (mode != AntialiasingOff) {
					g.t.Fatalf("%s %s: fractional edge count %d", orientation, mode, fractional)
				}
				dst.Deallocate()
			}
			if mode == AntialiasingFXAA {
				parent := ebiten.NewImage(80, 80)
				parent.Fill(color.RGBA{B: 255, A: 255})
				dst := parent.SubImage(image.Rect(7, 9, 71, 58)).(*ebiten.Image)
				viewport := Viewport{Y: -1, Height: 1}
				if orientation == Horizontal {
					viewport = Viewport{X: 0, Width: 1}
				}
				scene.DrawWithPasses(dst, viewport, RenderPasses{AboveRocks: func(c RenderContext) { c.Dst.Fill(color.White) }})
				for _, tc := range []struct {
					p    image.Point
					want color.RGBA
				}{{image.Pt(0, 0), color.RGBA{B: 255, A: 255}}, {image.Pt(7, 9), color.RGBA{255, 255, 255, 255}}, {image.Pt(70, 57), color.RGBA{255, 255, 255, 255}}} {
					if got := color.RGBAModel.Convert(parent.At(tc.p.X, tc.p.Y)).(color.RGBA); got != tc.want {
						g.t.Fatalf("subimage %v: %v want %v", tc.p, got, tc.want)
					}
				}
				parent.Deallocate()
			}
			scene.Close()
		}
	}
}
