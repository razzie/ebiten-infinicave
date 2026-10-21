package render

import (
	"bytes"
	"context"
	"math"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Read the actual shader output in a fresh game loop, as in the background test.
func TestFogContrastPixels(t *testing.T) {
	if os.Getenv("INFINICAVE_FOG_TEST") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFogContrastPixels$", "-test.count=1", "-test.v")
		cmd.Env = append(os.Environ(), "INFINICAVE_FOG_TEST=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fog pixel check: %v\n%s", err, output)
		}
		return
	}
	g := &fogTestGame{t: t}
	ebiten.SetWindowSize(64, 64)
	if err := ebiten.RunGame(g); err != nil {
		t.Fatal(err)
	}
}

type fogTestGame struct {
	t    *testing.T
	done bool
}

func (*fogTestGame) Layout(int, int) (int, int) { return 64, 64 }
func (g *fogTestGame) Update() error {
	if g.done {
		return ebiten.Termination
	}
	return nil
}
func (g *fogTestGame) Draw(*ebiten.Image) {
	if g.done {
		return
	}
	g.done = true
	const pixels = 128
	dst := ebiten.NewImage(pixels, 4*pixels)
	defer dst.Deallocate()
	data := make([]byte, pixels*4*pixels*4)
	for _, orientation := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		r, err := NewFogRenderer(1, orientation)
		if err != nil {
			g.t.Error(err)
			return
		}
		defer r.Close()
		var previous []byte
		for _, strength := range []float64{0, 1, 2, 10, math.MaxFloat64} {
			dst.Clear()
			r.Strength = strength
			for top := 0; top < 4; top++ {
				r.DrawSection(dst, float64(top), 0, RenderTransform{Pixels: pixels})
			}
			dst.ReadPixels(data)
			peak, clear, sum := 0, 0, 0
			for i := 3; i < len(data); i += 4 {
				a := int(data[i])
				peak = max(peak, a)
				sum += a
				if a <= 5 {
					clear++
				}
				if a > 154 {
					g.t.Fatalf("orientation %v strength %g: fog hides background (alpha %d)", orientation, strength, a)
				}
				if previous != nil && data[i] < previous[i] {
					g.t.Fatalf("orientation %v strength %g: increasing strength reduced opacity", orientation, strength)
				}
			}
			count := len(data) / 4
			if strength == 0 && peak != 0 {
				g.t.Fatal("zero strength produced visible mist")
			}
			if strength == 1 || strength == 2 {
				if peak < 64 || clear < count/10 || sum/count > peak/2 {
					g.t.Errorf("orientation %v strength %g: insufficient cloud contrast: peak=%d clear=%d/%d mean=%d", orientation, strength, peak, clear, count, sum/count)
				}
			}
			previous = append(previous[:0], data...)
		}
		g.checkWind(orientation)
	}
}

func (g *fogTestGame) checkWind(orientation terrain.Orientation) {
	g.t.Helper()
	r, err := NewFogRenderer(1, orientation)
	if err != nil {
		g.t.Fatal(err)
	}
	defer r.Close()
	const pixels = 128
	dst := ebiten.NewImage(pixels, pixels)
	defer dst.Deallocate()
	snapshot := func() []byte {
		dst.Clear()
		r.DrawSection(dst, 0, 0, RenderTransform{Pixels: pixels})
		data := make([]byte, pixels*pixels*4)
		dst.ReadPixels(data)
		return data
	}
	// Start with nonzero independent drift to catch position jumps at a change.
	for range 30 {
		r.Update()
	}
	before := snapshot()
	tps := ebiten.TPS()
	if tps <= 0 {
		tps = ebiten.DefaultTPS
	}
	for _, scale := range []float64{1, -1, 2} {
		r.SetVelocity(geom.V{X: .125 * float64(tps) * scale, Y: -.0625 * float64(tps) * scale})
		if !bytes.Equal(before, snapshot()) {
			g.t.Fatal("changing wind jumped the fog pattern")
		}
		r.Update()
		after := snapshot()
		dx, dy := int(16*scale), int(-8*scale)
		if orientation == terrain.Horizontal {
			dx, dy = dy, -dx
		}
		changed := false
		for y := 32; y < pixels-32; y++ {
			for x := 32; x < pixels-32; x++ {
				i := (y*pixels + x) * 4
				j := ((y-dy)*pixels + x - dx) * 4
				for c := range 4 {
					// Allow one byte for GPU floating-point rounding.
					if math.Abs(float64(after[i+c])-float64(before[j+c])) > 1 {
						g.t.Fatalf("%v scale %g: fog did not follow world velocity at %d,%d", orientation, scale, x, y)
					}
				}
				changed = changed || after[i+3] != before[i+3]
			}
		}
		if !changed {
			g.t.Fatal("wind did not move fog")
		}
		before = after
	}
	r.SetVelocity(geom.V{})
	for range 60 {
		r.Update()
	}
	if !bytes.Equal(before, snapshot()) {
		g.t.Fatal("zero velocity did not freeze clouds and wisps")
	}
}
