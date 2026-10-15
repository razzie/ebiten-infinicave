package infinicave

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Called inside the carving pixel test's child game loop so uploads, shaders,
// orientation, and edits are checked against actual GPU output.
func checkAppearanceFadePixels(t *testing.T) {
	t.Helper()
	checkAppearanceFadeBands(t)
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		for _, aa := range []AntialiasingMode{AntialiasingFull, AntialiasingOff, AntialiasingFXAA} {
			scene, err := NewScene(Config{AppearanceFade: true, DecorationBurn: true, DisableBackground: true, Orientation: orientation, Antialiasing: aa})
			if err != nil {
				t.Fatal(err)
			}
			defer scene.Close()
			scene.world.close()
			w := &world{sections: make(map[int64]*worldSection), done: make(chan struct{}), pixels: 256,
				antialiasing: aa, disableBackground: true, appearanceFade: scene.appearanceFade, working: true}
			scene.world = w
			data := carvePixelFixture()
			geometry := terrain.PrepareTerrainGeometry(data, 0)
			growth := render.RockGrowthOptions{Moss: 1, Hanging: 1, Grass: 1}
			base := render.PrepareRockBase(data, ViewShaded, growth)
			base.Geometry = geometry
			render.FinishSectionMesh(&base)
			publish := func(mesh render.SectionMesh) {
				w.startUpload(mesh)
				for i := 0; i < 500 && w.upload != nil; i++ {
					w.receive(scene)
				}
				if w.upload != nil {
					t.Fatal("appearance fixture upload stalled")
				}
			}
			publish(base)
			s := w.sections[0]
			viewport := Viewport{Y: -1, Height: 1}
			if orientation == Horizontal {
				viewport = Viewport{Width: 1}
			}
			draw := func() []byte {
				img := ebiten.NewImage(256, 256)
				defer img.Deallocate()
				scene.Draw(img, viewport)
				out := make([]byte, 256*256*4)
				img.ReadPixels(out)
				return out
			}
			count := func(pixels []byte) int {
				n := 0
				for i := 3; i < len(pixels); i += 4 {
					if pixels[i] > 0 {
						n++
					}
				}
				return n
			}
			advance := func(ticks int) {
				for i := 0; i < ticks; i++ {
					scene.Update(viewport)
				}
			}
			if count(draw()) != 0 {
				t.Fatal("new foreground did not start hidden")
			}
			advance(21)
			middle := draw()
			w.appearanceFade = nil
			complete := draw()
			w.appearanceFade = scene.appearanceFade
			if !bytes.Equal(middle, draw()) {
				t.Fatal("Draw advanced the fade")
			}
			var fadedAlpha, fullAlpha int
			for i := 0; i < len(middle); i++ {
				if i%4 == 3 {
					fadedAlpha += int(middle[i])
					fullAlpha += int(complete[i])
				}
				// A plain fade scales premultiplied color and alpha equally,
				// without adding a glow or spatial dissolve pattern.
				if aa != AntialiasingFXAA && (int(middle[i])*2-int(complete[i]) > 2 || int(complete[i])-int(middle[i])*2 > 2) {
					t.Fatal("arrival did not fade color and alpha uniformly")
				}
			}
			if fullAlpha == 0 || float64(fadedAlpha)/float64(fullAlpha) < .48 || float64(fadedAlpha)/float64(fullAlpha) > .52 {
				t.Fatal("halfway fade did not produce half opacity")
			}
			// Growth may finish while the rock's initial reveal is still active.
			full := render.PrepareSection(data, ViewShaded, growth)
			full.Geometry = geometry
			render.FinishSectionMesh(&full)
			rock, trim, progress := s.foreground, s.trim, s.foregroundReveal.Progress()
			publish(full)
			if !bytes.Equal(middle, draw()) || s.foreground != rock || s.trim != trim || s.foregroundReveal.Progress() != progress || s.foregroundReveal.Previous != nil {
				t.Fatal("growth publication changed structural pixels, ownership, or reveal progress")
			}
			point := V{X: .45, Y: -.35}
			if _, err := scene.CarveCircle(terrain.WorldPoint(orientation, point), .075); err != nil {
				t.Fatal(err)
			}
			if s.geometry.Collision.Contains(point) {
				t.Fatal("appearance delayed collision removal")
			}
			// Every published span respects the cut immediately.
			for _, layer := range []*ebiten.Image{s.foreground, s.trim, s.growth[underGrowth].image, s.growth[surfaceGrowth].image} {
				if layer != nil {
					_, _, _, alpha := layer.At(115, 166).RGBA()
					if alpha != 0 {
						t.Fatal("a foreground span retained cut pixels")
					}
				}
			}
			advance(50)
			settled := draw()
			w.appearanceFade = nil
			if !bytes.Equal(settled, draw()) || s.foregroundReveal.Previous != nil {
				t.Fatal("finished reveal differs from cached layers or retains its snapshot")
			}
			w.appearanceFade = scene.appearanceFade
			scene.SetRenderWidth(384)
			publish(*s.mesh)
			if s.foregroundReveal.Progress() != 1 || s.grassReveal.Progress() != 1 || s.plantReveal(foregroundVinesProduct).Progress() != 1 {
				t.Fatal("resize restarted an appearance effect")
			}
			// Reloading an evicted section reveals its cut version, never the old rock.
			s.deallocate()
			delete(w.sections, 0)
			publish(full)
			if w.sections[0].foregroundReveal.Progress() != 0 || count(draw()) != 0 {
				t.Fatal("reloaded section did not reveal from its stored cuts")
			}
			root := w.sections[0].mesh.Plants[0].Anchors[0].Add(V{Y: -1})
			if _, err := scene.CarveCircle(terrain.WorldPoint(orientation, root), .006); err != nil {
				t.Fatal(err)
			}
			if count(draw()) != 0 {
				t.Fatal("carving flashed decorations that had not appeared yet")
			}
		}
	}
}

func checkAppearanceFadeBands(t *testing.T) {
	t.Helper()
	r, err := render.NewFadeRenderer()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	w := &world{sections: make(map[int64]*worldSection), appearanceFade: r}
	defer func() {
		for _, s := range w.sections {
			s.deallocate()
		}
	}()
	for id := int64(0); id < 2; id++ {
		s := &worldSection{foreground: ebiten.NewImage(64, 64), terrain: ebiten.NewImage(128, 64), pixels: 64}
		s.foreground.Fill(color.White)
		s.terrain.Fill(color.White)
		s.foregroundReveal.Start(nil)
		s.backgroundReveal.Start(nil)
		s.plantReveal(foregroundVinesProduct).Start(nil)
		for i := 0; i < 21; i++ {
			s.foregroundReveal.Update()
			s.backgroundReveal.Update()
			s.plantReveal(foregroundVinesProduct).Update()
		}
		w.sections[id] = s
	}
	white := ebiten.NewImage(64, 128)
	white.Fill(color.White)
	defer white.Deallocate()
	want, got := ebiten.NewImage(64, 128), ebiten.NewImage(64, 128)
	defer want.Deallocate()
	defer got.Deallocate()
	view := render.RenderTransform{Pixels: 64}
	compare := func(message string) {
		a, b := make([]byte, 64*128*4), make([]byte, 64*128*4)
		want.ReadPixels(a)
		got.ReadPixels(b)
		if !bytes.Equal(a, b) {
			t.Fatal(message)
		}
		got.Clear()
	}
	r.DrawReveal(want, white, nil, .5, ebiten.GeoM{})
	w.DrawForegroundSilhouette(got, -2, 2, 0, view)
	compare("rock/shadow fade changed across section bands")
	w.DrawBackground(got, -2, 2, view)
	compare("background fade changed across section bands")
	want.Clear()
	vine := ebiten.NewImage(32, 128)
	vine.Fill(color.White)
	defer vine.Deallocate()
	bounds := image.Rect(16, 0, 48, 128)
	var transform ebiten.GeoM
	transform.Translate(16, 0)
	r.DrawReveal(want, vine, nil, .5, transform)
	w.drawSupportedVegetation(got, vine, bounds, -2, -2, 2, 64, view, false, w.sections[0].plantReveal(foregroundVinesProduct))
	compare("cropped crossing plants changed the fade opacity")
	w.sections[1].foregroundReveal.Start(nil)
	w.drawSupportedVegetation(got, vine, bounds, -2, -2, 2, 64, view, false, w.sections[0].plantReveal(foregroundVinesProduct))
	pixels := make([]byte, 64*128*4)
	got.ReadPixels(pixels)
	for i := 3; i < 64*64*4; i += 4 {
		if pixels[i] != 0 {
			t.Fatal("crossing vegetation appeared before its supporting band")
		}
	}
}
