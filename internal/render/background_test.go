package render

import (
	"context"
	"image"
	"image/color"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Pixel readback needs a game loop in its own process.
func TestBackgroundShadowPixels(t *testing.T) {
	if os.Getenv("INFINICAVE_BACKGROUND_TEST") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBackgroundShadowPixels$", "-test.count=1")
		cmd.Env = append(os.Environ(), "INFINICAVE_BACKGROUND_TEST=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("background pixel check: %v\n%s", err, output)
		}
		return
	}
	g := &backgroundTestGame{t: t}
	ebiten.SetWindowSize(64, 64)
	if err := ebiten.RunGame(g); err != nil {
		t.Fatal(err)
	}
}

type backgroundTestGame struct {
	t    *testing.T
	done bool
}

func (*backgroundTestGame) Layout(int, int) (int, int) { return 64, 64 }
func (g *backgroundTestGame) Update() error {
	if g.done {
		return ebiten.Termination
	}
	return nil
}
func (g *backgroundTestGame) Draw(*ebiten.Image) {
	if g.done {
		return
	}
	g.done = true
	r, err := NewBackgroundRenderer(BackgroundConfig{ShadowOpacity: 1})
	if err != nil {
		g.t.Error(err)
		return
	}
	defer r.Close()
	mask := ebiten.NewImage(64, 64)
	defer mask.Deallocate()
	alpha := func(img *ebiten.Image, x, y int) uint32 {
		_, _, _, a := img.At(x, y).RGBA()
		return a / 257
	}
	// A one-pixel ledge must block every pixel below it, even when the
	// projection is much longer than the ledge is thick. Test both axes,
	// reversed/custom directions, and subpixel diagonal sample spacing.
	for _, direction := range []geom.V{{Y: 1}, {X: 1}, {Y: -1}, {X: -1}, {X: 1, Y: 1}} {
		mask.Clear()
		mask.SubImage(image.Rect(30, 30, 31, 31)).(*ebiten.Image).Fill(color.White)
		r.Offset = direction.Mul(17.0 / 64)
		shadow := r.sweepShadow(mask, 64)
		for distance := 0; distance <= 17; distance++ {
			x, y := 30+int(direction.X)*distance, 30+int(direction.Y)*distance
			if a := alpha(shadow, x, y); a != 255 {
				g.t.Errorf("direction %v: light leak at distance %d: alpha %d", direction, distance, a)
			}
		}
		if a := alpha(shadow, 30+int(direction.X)*20, 30+int(direction.Y)*20); a != 0 {
			g.t.Errorf("direction %v: shadow exceeded configured reach", direction)
		}
	}
	// Maximum coverage must preserve partial appearance fades, including
	// overlap, and erasing the caster must remove its entire shadow.
	mask.Clear()
	mask.SubImage(image.Rect(20, 10, 24, 14)).(*ebiten.Image).Fill(color.RGBA{A: 100})
	r.Offset = geom.V{Y: .25}
	if a := alpha(r.sweepShadow(mask, 64), 22, 20); a != 100 {
		g.t.Errorf("shadow changed reveal opacity: %d", a)
	}
	mask.Clear()
	if a := alpha(r.sweepShadow(mask, 64), 22, 20); a != 0 {
		g.t.Error("removed caster retained a shadow")
	}

	layers := &backgroundTestLayers{background: ebiten.NewImage(64, 64), mask: mask, lighting: ebiten.NewImage(64, 64)}
	defer layers.background.Deallocate()
	defer layers.lighting.Deallocate()
	layers.background.Fill(color.RGBA{R: 200, G: 200, B: 200, A: 255})
	// Left faces the light; right faces away. Both receive the same shadow.
	for i, normal := range []geom.V3{{Y: -.8, Z: .6}, {Y: .8, Z: .6}} {
		x := float64(i) * .032
		poly := []geom.V{{X: x}, {X: x + .032}, {X: x + .032, Y: .064}, {X: x, Y: .064}}
		vertices, indices := appendCellMesh(nil, nil, poly, geom.PolygonCenter(poly), color.NRGBA{A: 255}, normal)
		mesh := TranslateMesh(TriangleMesh{Vertices: vertices, Indices: indices}, 0, float32(terrain.GenerationMinY*RasterPixelsPerUnit))
		r.DrawLighting(layers.lighting, mesh, geom.V3{Y: -1})
	}
	mask.SubImage(image.Rect(8, 16, 56, 18)).(*ebiten.Image).Fill(color.White)
	dst := ebiten.NewImage(64, 64)
	defer dst.Deallocate()
	r.Draw(dst, layers, 0, 1, TargetTransform(dst))
	red := func(img *ebiten.Image, x, y int) uint32 {
		v, _, _, _ := img.At(x, y).RGBA()
		return v / 257
	}
	if lit, shaded, away := red(dst, 16, 8), red(dst, 16, 24), red(dst, 48, 24); lit != 200 || shaded >= 100 || shaded <= 0 || away != 200 {
		g.t.Errorf("normal-aware shading: lit=%d shadow=%d away=%d", lit, shaded, away)
	}
	// An upstream caster outside the viewport must still shadow visible rock.
	dst.Clear()
	r.Draw(dst, layers, .3125, 1, TargetTransform(dst))
	if v := red(dst, 16, 4); v >= 100 {
		g.t.Errorf("offscreen caster lost its shadow: %d", v)
	}
	// Horizontal scenes project down screen-space Y along internal X. The
	// cross-axis padding must include casters left of the intermediate target.
	mask.Clear()
	mask.SubImage(image.Rect(16, 8, 18, 56)).(*ebiten.Image).Fill(color.White)
	r.Offset = geom.V{X: .25}
	dst.Clear()
	r.Draw(dst, layers, 0, 1, RenderTransform{Pixels: 64, OffsetX: -20})
	if v := red(dst, 4, 16); v >= 100 {
		g.t.Errorf("offscreen cross-axis caster lost its shadow: %d", v)
	}
	mask.Clear()
	mask.SubImage(image.Rect(8, 16, 56, 18)).(*ebiten.Image).Fill(color.White)
	r.Offset = geom.V{Y: .25}
	// Resizing effect buffers preserves the same world-space shadow.
	large := ebiten.NewImage(128, 128)
	defer large.Deallocate()
	r.Draw(large, layers, 0, 1, TargetTransform(large))
	if v := red(large, 32, 48); v >= 100 {
		g.t.Errorf("resize lost shadow: %d", v)
	}
	// Missing rear rock remains transparent even with blur and occlusion.
	layers.background.SubImage(image.Rect(0, 40, 64, 64)).(*ebiten.Image).Clear()
	r.softness, r.shadowSoftness = .02, .02
	dst.Clear()
	r.Draw(dst, layers, 0, 1, TargetTransform(dst))
	if a := alpha(dst, 16, 42); a != 0 {
		g.t.Errorf("blur filled missing rear terrain: alpha %d", a)
	}
}

type backgroundTestLayers struct {
	background, mask, lighting *ebiten.Image
}

func (l *backgroundTestLayers) draw(dst, src *ebiten.Image, y float64, view RenderTransform) {
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(float64(view.Pixels)/64, float64(view.Pixels)/64)
	op.GeoM.Translate(view.OffsetX, -y*float64(view.Pixels))
	dst.DrawImage(src, op)
}
func (l *backgroundTestLayers) DrawBackground(dst *ebiten.Image, y, height float64, view RenderTransform) {
	l.draw(dst, l.background, y, view)
}
func (l *backgroundTestLayers) DrawForegroundSilhouette(dst *ebiten.Image, y, height, offsetX float64, view RenderTransform) {
	view.OffsetX += offsetX * float64(view.Pixels)
	l.draw(dst, l.mask, y, view)
}
func (l *backgroundTestLayers) DrawBackgroundLighting(dst *ebiten.Image, y, height float64, view RenderTransform) {
	l.draw(dst, l.lighting, y, view)
}
