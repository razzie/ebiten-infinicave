package render

import (
	"context"
	"math"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
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
	}
}
