package infinicave

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Readback in a child game loop verifies the actual GPU result, including
// atlas sampling, partial uploads, dirty tiles, masks, and resolution replay.
func TestCarveCachedPixelsAndUploadReplay(t *testing.T) {
	if os.Getenv("INFINICAVE_CARVE_PIXELS") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCarveCachedPixelsAndUploadReplay$", "-test.count=1")
		cmd.Env = append(os.Environ(), "INFINICAVE_CARVE_PIXELS=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("rendered carving: %v\n%s", err, out)
		}
		return
	}
	g := &carvePixelGame{t: t}
	ebiten.SetWindowSize(256, 256)
	if err := ebiten.RunGame(g); err != nil {
		t.Fatal(err)
	}
}

type carvePixelGame struct {
	t    *testing.T
	done bool
}

func (g *carvePixelGame) Layout(int, int) (int, int) { return 256, 256 }
func (g *carvePixelGame) Update() error {
	if g.done {
		return ebiten.Termination
	}
	return nil
}
func (g *carvePixelGame) Draw(*ebiten.Image) {
	if g.done {
		return
	}
	g.done = true
	checkAppearanceFadePixels(g.t)
	checkDecorationBurnPixels(g.t)
	checkPartialVegetationCarvePixels(g.t)
	checkIndependentGrassPixels(g.t)
	checkGrowthCompositionPixels(g.t)
	scene, err := NewScene(Config{View: ViewShaded})
	if err != nil {
		g.t.Fatal(err)
	}
	defer scene.Close()
	data := carvePixelFixture()
	finish := func() {
		scene.flushCarveImages()
		for i := 0; i < 500 && scene.world.upload != nil; i++ {
			scene.world.receive(scene)
		}
		if scene.world.upload != nil {
			g.t.Fatal("upload stalled")
		}
	}
	alpha := func(img *ebiten.Image, p V, pixels int) uint8 {
		if img == nil {
			return 0
		}
		return color.RGBAModel.Convert(img.At(int(p.X*float64(pixels)), int(p.Y*float64(pixels)))).(color.RGBA).A
	}
	checkVines := func(s *worldSection, pixels int) {
		g.t.Helper()
		for _, layer := range []struct {
			img                 *ebiten.Image
			bounds              image.Rectangle
			cutPoint, keptPoint V
			carved              bool
		}{
			{s.vines, s.vinesBounds, V{X: .45, Y: .6}, V{X: .75, Y: .6}, false},
			{s.foregroundVines, s.foregroundVinesBounds, V{X: .45, Y: .6125}, V{X: .72, Y: .68}, true},
		} {
			if layer.img == nil {
				g.t.Fatal("vine layer disappeared")
			}
			read := func(p V) uint8 {
				x := int(p.X*float64(pixels)) - layer.bounds.Min.X
				y := int((p.Y-terrain.GenerationMinY)*float64(pixels)) - layer.bounds.Min.Y
				return color.RGBAModel.Convert(layer.img.At(x, y)).(color.RGBA).A
			}
			if (read(layer.cutPoint) == 0) != layer.carved || read(layer.keptPoint) == 0 {
				g.t.Fatalf("vine layer coverage at width %d disagrees with carved=%v", pixels, layer.carved)
			}
		}
		if !reflect.DeepEqual(s.geometry.Vegetation.Vines, data.Vines) || !reflect.DeepEqual(s.geometry.Vegetation.ForegroundVines, data.ForegroundVines) {
			g.t.Fatal("texture masking changed CPU vine geometry")
		}
	}
	readPixels := func(img *ebiten.Image) []byte {
		pixels := make([]byte, 4*img.Bounds().Dx()*img.Bounds().Dy())
		img.ReadPixels(pixels)
		return pixels
	}
	for stage := uploadRockUnderlay; stage <= uploadPlantsPublish; stage++ {
		scene.world.close()
		scene.world = &world{sections: make(map[int64]*worldSection), done: make(chan struct{}), pixels: 256, collision: map[int64]*terrain.Geometry{0: terrain.PrepareTerrainGeometry(data, 0)}}
		mesh := render.PrepareSection(data, ViewShaded, render.RockGrowthOptions{Moss: 1, Hanging: 1, Grass: 1})
		mesh.Geometry = terrain.PrepareTerrainGeometry(data, 0)
		render.FinishSectionMesh(&mesh)
		scene.world.startUpload(mesh)
		for i := 0; i < 500 && scene.world.upload != nil && scene.world.upload.stage < stage; i++ {
			scene.world.receive(scene)
		}
		if _, err := scene.CarveCircle(V{X: .45, Y: -.35}, .075); err != nil {
			g.t.Fatal(err)
		}
		finish()
		s := scene.world.sections[0]
		if s == nil || alpha(s.foreground, V{X: .45, Y: .65}, 256) != 0 || alpha(s.foreground, V{X: .85, Y: .75}, 256) == 0 {
			g.t.Fatalf("stage %d: cut mask or unaffected rock disappeared", stage)
		}
		if s.geometry.Collision.Contains(V{X: .45, Y: -.35}) {
			g.t.Fatalf("stage %d: collision refilled the visible cut", stage)
		}
		checkVines(s, 256)
		backgroundVines := readPixels(s.vines)
		for _, a := range s.mesh.Foreground.Growth {
			if a.Kind == render.SurfaceMoss && a.Dead {
				g.t.Fatal("texture erasure deleted the moss recipe")
			}
		}
		// Atlas cells must contain visible grass/moss, not transparent quads.
		if s.grass == nil {
			g.t.Fatalf("stage %d: grass atlas did not render", stage)
		}
		visible := false
		for y := 70; y < 95; y++ {
			for x := 10; x < 245; x++ {
				if color.RGBAModel.Convert(s.grass.At(x, y)).(color.RGBA).A > 0 {
					visible = true
					break
				}
			}
		}
		if !visible {
			g.t.Fatalf("stage %d: grass samples are blank", stage)
		}
		before := s.foreground.At(217, 192)
		// Repeated exact commands do not accumulate edge erasure or history.
		count := len(scene.world.cuts)
		if _, err := scene.CarveCircle(V{X: .45, Y: -.35}, .075); err != nil {
			g.t.Fatal(err)
		}
		if len(scene.world.cuts) != count || !reflect.DeepEqual(before, s.foreground.At(217, 192)) {
			g.t.Fatal("repeated cut changed untouched pixels or command history")
		}
		if _, err := scene.CarveCircle(V{X: .49, Y: -.35}, .055); err != nil {
			g.t.Fatal(err)
		}
		scene.flushCarveImages()
		if alpha(s.foreground, V{X: .45, Y: .65}, 256) != 0 || !reflect.DeepEqual(before, s.foreground.At(217, 192)) {
			g.t.Fatalf("stage %d: overlapping tile replay changed untouched rock", stage)
		}
		// Restoring tiles must not create opaque seams inside an existing cut.
		for y := 0; y < 256; y++ {
			for x := 0; x < 256; x++ {
				p := V{X: (float64(x) + .5) / 256, Y: (float64(y) + .5) / 256}
				if p.Sub(V{X: .45, Y: .65}).Len() < .05 && color.RGBAModel.Convert(s.foreground.At(x, y)).(color.RGBA).A != 0 {
					g.t.Fatalf("opaque cut interior at (%d,%d) after tile replay", x, y)
				}
			}
		}
		checkVines(s, 256)
		if !bytes.Equal(backgroundVines, readPixels(s.vines)) {
			g.t.Fatal("overlapping foreground cuts changed background vine pixels")
		}
		if stage == uploadPlantsPublish {
			// Removing one attachment preserves a nearby overlapping survivor.
			if len(s.mesh.Plants) < 2 {
				g.t.Fatal("fixture has too few mushroom groups")
			}
			root := s.mesh.Plants[0].Anchors[0]
			if _, err := scene.CarveCircle(root.Add(V{Y: -1}), .006); err != nil {
				g.t.Fatal(err)
			}
			scene.flushCarveImages()
			if !s.mesh.Plants[0].Dead || s.mesh.Plants[len(s.mesh.Plants)-1].Dead {
				g.t.Fatal("attachment removal damaged a neighboring group")
			}
			if len(s.mesh.Webs) > 0 {
				p := data.ForegroundTopology.SpiderWebs[0].Network.Nodes[data.ForegroundTopology.SpiderWebs[0].Network.Anchors]
				if _, err := scene.CarveCircle(p.Add(V{Y: -1}), .004); err != nil {
					g.t.Fatal(err)
				}
				scene.flushCarveImages()
				if s.mesh.Webs[0].Dead {
					g.t.Fatal("free-node damage removed a supported web")
				}
				if _, err := scene.CarveCircle(s.mesh.Webs[0].Anchors[0].Add(V{Y: -1}), .002); err != nil {
					g.t.Fatal(err)
				}
				scene.flushCarveImages()
				if !s.mesh.Webs[0].Dead {
					g.t.Fatal("support damage retained a web")
				}
			}
			scene.SetRenderWidth(384)
			scene.world.startUpload(*s.mesh)
			finish()
			if alpha(s.foreground, V{X: .45, Y: .65}, 384) != 0 || alpha(s.foreground, V{X: .85, Y: .75}, 384) == 0 {
				g.t.Fatal("resize refilled a cut or lost rock")
			}
			checkVines(s, 384)
			backgroundVines = readPixels(s.vines)
			fresh := render.PrepareSection(data, ViewShaded, render.RockGrowthOptions{Moss: 1, Hanging: 1, Grass: 1})
			fresh.Geometry = terrain.PrepareTerrainGeometry(data, 0)
			render.FinishSectionMesh(&fresh)
			s.deallocate()
			delete(scene.world.sections, 0)
			scene.world.revision++
			scene.world.startUpload(fresh)
			finish()
			reloaded := scene.world.sections[0]
			if alpha(reloaded.foreground, V{X: .45, Y: .65}, 384) != 0 || !reloaded.mesh.Plants[0].Dead || !reloaded.mesh.Webs[0].Dead {
				g.t.Fatal("reload restored a cut or damaged attachment")
			}
			checkVines(reloaded, 384)
			if !bytes.Equal(backgroundVines, readPixels(reloaded.vines)) {
				g.t.Fatal("stored foreground cuts changed background vine pixels on reload")
			}
		}
	}
}

func carvePixelFixture() terrain.SectionData {
	cell := terrainRect(.05, .35, .90, .55)
	cell.Normal = V3{Y: -.6, Z: .8}
	cell.Color = color.NRGBA{R: 130, G: 120, B: 90, A: 255}
	cell.Shadow, cell.Ambient, cell.Z = 1, 1, .025
	topology := terrain.NewRockTopology(RockGrid{cell})
	topology.Appearance = topology.Grid
	web := terrain.NewSpiderWeb([]V{{X: .15, Y: .12}, {X: .33, Y: .12}, {X: .18, Y: .27}}, .4)
	topology.SpiderWebs = []terrain.SpiderWeb{web}
	guide := terrain.SplineGuide([]V{{X: .05, Y: .35}, {X: .95, Y: .35}}, 1)
	guide.Seed = 42
	back := terrainRect(-.5, -1, 2, 3)
	back.Raised = false
	back.Color = color.NRGBA{R: 20, G: 25, B: 20, A: 255}
	return terrain.SectionData{Foreground: topology.Grid, ForegroundTopology: topology, Background: RockGrid{back}, Guides: []Guide{guide}, Mushrooms: terrain.MushroomsForGuides([]Guide{guide}, topology.Grid, 1),
		Vines: []Vine{{Parent: -1, Points: []VinePoint{{P: V{X: .1, Y: .6}, Radius: .004}, {P: V{X: .9, Y: .6}, Radius: .004}}}}, ForegroundVines: []Vine{{Parent: -1, Foreground: true, Points: []VinePoint{{P: V{X: .2, Y: .55}, Radius: .0014}, {P: V{X: .8, Y: .7}, Radius: .0014}}}}}
}

// API latency benchmark uses already baked meshes and synthetic GPU caches;
// initialization/generation are excluded. Pixel tests above check correctness.
func BenchmarkCarveWithCachedImages(b *testing.B) {
	data := carvePixelFixture()
	prepared := render.PrepareSection(data, ViewShaded, render.RockGrowthOptions{Moss: 1, Grass: 1})
	prepared.Geometry = terrain.PrepareTerrainGeometry(data, 0)
	render.FinishSectionMesh(&prepared)
	shader, err := ebiten.NewShader(render.MaterialShaderSource)
	if err != nil {
		b.Fatal(err)
	}
	defer shader.Deallocate()
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		mesh := prepared
		w := &world{sections: map[int64]*worldSection{0: {foreground: ebiten.NewImage(256, 256), mesh: &mesh, geometry: prepared.Geometry, pixels: 256}}, done: make(chan struct{})}
		g := &Scene{world: w, material: shader, view: ViewShaded}
		w.queryIndex()
		b.StartTimer()
		if _, err := g.CarveCircle(V{X: .45, Y: -.35}, .075); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		w.close()
		b.StartTimer()
	}
}
