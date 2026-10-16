package infinicave

import (
	"bytes"
	"image/color"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Runs inside the carving pixel test's child game loop, so readback exercises
// the actual shader, atlas textures, and orientation compositor.
func checkDecorationBurnPixels(t *testing.T) {
	t.Helper()
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		for _, aa := range []AntialiasingMode{AntialiasingFull, AntialiasingOff, AntialiasingFXAA} {
			scene, err := NewScene(Config{DecorationBurn: true, DisableBackground: true, Orientation: orientation, Antialiasing: aa})
			if err != nil {
				t.Fatal(err)
			}
			defer scene.Close()
			scene.world.close()
			data := carvePixelFixture()
			scene.world = &world{sections: make(map[int64]*worldSection), done: make(chan struct{}), pixels: 256, antialiasing: aa, disableBackground: true}
			mesh := render.PrepareSection(data, ViewShaded, render.RockGrowthOptions{Moss: 1, Hanging: 1, Grass: 1})
			mesh.Geometry = terrain.PrepareTerrainGeometry(data, 0)
			render.FinishSectionMesh(&mesh)
			finish := func() {
				for i := 0; i < 500 && scene.world.upload != nil; i++ {
					scene.world.receive(scene)
				}
				if scene.world.upload != nil {
					t.Fatal("burn fixture upload stalled")
				}
			}
			scene.world.startUpload(mesh)
			finish()
			s := scene.world.sections[0]
			carve := func(point V, radius float64) {
				if _, err := scene.CarveCircle(terrain.WorldPoint(orientation, point), radius); err != nil {
					t.Fatal(err)
				}
			}
			carve(V{X: .45, Y: -.35}, .075) // includes a foreground vine
			root := s.mesh.Plants[0].Anchors[0].Add(V{Y: -1})
			carve(root, .006)
			carve(s.mesh.Webs[0].Anchors[0].Add(V{Y: -1}), .004)
			scene.flushCarveImages()
			if !s.mesh.Plants[0].Dead || !s.mesh.Webs[0].Dead || s.geometry.Collision.Contains(root) {
				t.Fatal("burn delayed decoration or collision removal")
			}
			burn := scene.decorationBurn
			draw := func(pixels int) []byte {
				img := ebiten.NewImage(pixels, pixels)
				defer img.Deallocate()
				burn.Draw(img, -1, render.RenderTransform{Pixels: pixels}, false)
				burn.Draw(img, -1, render.RenderTransform{Pixels: pixels}, true)
				out := make([]byte, pixels*pixels*4)
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
			initial := draw(256)
			if count(initial) == 0 || !bytes.Equal(initial, draw(256)) {
				t.Fatal("burn is blank or drawing advances animation")
			}
			carve(root, .006)
			if !bytes.Equal(initial, draw(256)) {
				t.Fatal("repeated cut restarted a burn")
			}
			viewport := Viewport{Y: -1, Height: 1}
			if orientation == Horizontal {
				viewport = Viewport{Width: 1}
			}
			with, without := ebiten.NewImage(256, 256), ebiten.NewImage(256, 256)
			scene.Draw(with, viewport)
			scene.decorationBurn = nil
			scene.Draw(without, viewport)
			scene.decorationBurn = burn
			a, b := make([]byte, 256*256*4), make([]byte, 256*256*4)
			with.ReadPixels(a)
			without.ReadPixels(b)
			with.Deallocate()
			without.Deallocate()
			if bytes.Equal(a, b) {
				t.Fatalf("orientation %v / AA %v: scene omitted burn", orientation, aa)
			}
			scene.SetRenderWidth(384)
			if count(draw(384)) <= count(initial) {
				t.Fatal("resizing lost or failed to scale existing burn")
			}
			for i := 0; i < 21; i++ {
				burn.Update()
			}
			middle := draw(256)
			if n := count(middle); n == 0 || n >= count(initial) {
				t.Fatal("burn did not progressively dissolve")
			}
			embers := 0
			for i := 0; i < len(middle); i += 4 {
				if middle[i] > middle[i+1] && middle[i+1] > middle[i+2] && middle[i] > 100 {
					embers++
				}
			}
			if embers == 0 {
				t.Fatal("burn has no glowing ember edge")
			}
			for i := 0; i < 30; i++ {
				burn.Update()
			}
			if count(draw(256)) != 0 {
				t.Fatal("expired burn retained pixels")
			}
			// Reloading stored cuts removes decoration without restarting an effect.
			s.deallocate()
			delete(scene.world.sections, 0)
			scene.world.startUpload(mesh)
			finish()
			if count(draw(256)) != 0 || !scene.world.sections[0].mesh.Plants[0].Dead {
				t.Fatal("stored cuts restarted burn or restored a removed mushroom")
			}
			patch := ebiten.NewImage(10, 10)
			patch.Fill(color.White)
			burn.Add(patch, V{X: .5, Y: -.5}, 256, false)
			scene.Reset(42)
			if count(draw(256)) != 0 {
				t.Fatal("reset retained burn snapshots")
			}
		}
	}
}
