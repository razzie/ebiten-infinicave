package infinicave

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestCarveReturnsWithGeometryBeforeTextureReplay(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		for _, entry := range []string{"circle", "segment", "hole", "terrain"} {
			for _, flush := range []string{"update", "draw"} {
				t.Run(strconv.Itoa(int(orientation))+"/"+entry+"/"+flush, func(t *testing.T) {
					g := resolutionTestScene(t)
					g.orientation = orientation
					w, s := g.world, g.world.sections[0]
					w.retainSections = len(w.sections)
					recipe := s.mesh
					point := terrain.WorldPoint(orientation, V{X: .5, Y: -.5})
					before := rockAt(t, g, point).Hit.FormationID
					carve := func() (CarveResult, error) {
						switch entry {
						case "circle":
							return g.CarveCircle(point, .08)
						case "segment":
							return g.CarveSegment(
								terrain.WorldPoint(orientation, V{X: .46, Y: -.5}),
								terrain.WorldPoint(orientation, V{X: .54, Y: -.5}), .16)
						case "terrain":
							return g.CarveTerrain(Hole{Shape: HoleCircle, Center: point, Radius: .08})
						default:
							return g.Carve(Hole{Shape: HoleCircle, Center: point, Radius: .08})
						}
					}
					result, err := carve()
					changes := 1
					if entry == "terrain" {
						changes = 0
						if w.queries != nil {
							t.Fatal("terrain cut eagerly rebuilt the query index")
						}
					}
					if err != nil || !result.Complete || len(result.Changes) != changes || len(result.SectionIDs) != 1 {
						t.Fatalf("immediate carve result: %+v, %v", result, err)
					}
					collision, ok := g.CollisionGeometry(0)
					if !ok || collision.Contains(point) || rockAt(t, g, point).Found {
						t.Fatal("carve returned before collision and queries reflected the cut")
					}
					if _, ok := g.Formation(before); ok {
						t.Fatal("carve returned before expiring the affected formation")
					}
					if len(w.carveImages) != 1 || s.mesh != recipe || w.white != nil {
						t.Fatal("carve performed texture work before returning")
					}
					if _, err := carve(); err != nil || len(w.carveImages) != 1 {
						t.Fatal("duplicate cut queued more texture work")
					}
					geometry, revision := s.geometry, g.GeometryRevision()
					viewport := Viewport{Y: -1, Height: 1}
					if orientation == Horizontal {
						viewport = Viewport{Width: 1}
					}
					if flush == "update" {
						g.Update(viewport)
					} else {
						dst := ebiten.NewImage(32, 32)
						defer dst.Deallocate()
						g.DrawWithPasses(dst, viewport, RenderPasses{BehindRocks: func(RenderContext) {
							if len(w.carveImages) != 0 || len(s.mesh.VisualCuts) != 1 {
								t.Fatal("draw used images before replaying the cut")
							}
						}})
					}
					if len(w.carveImages) != 0 || len(s.mesh.VisualCuts) != 1 || s.mesh.Geometry != geometry {
						t.Fatal("update/draw did not apply queued texture work")
					}
					if s.geometry != geometry || g.GeometryRevision() != revision {
						t.Fatal("texture replay changed published geometry or its revision")
					}
				})
			}
		}
	}
}

func TestCarveTextureQueueDiscardedWithWorld(t *testing.T) {
	for _, action := range []string{"reset", "close"} {
		t.Run(action, func(t *testing.T) {
			g := resolutionTestScene(t)
			w := g.world
			if _, err := g.CarveCircle(V{X: .5, Y: -.5}, .08); err != nil {
				t.Fatal(err)
			}
			if len(w.carveImages) != 1 {
				t.Fatal("fixture did not queue texture work")
			}
			if action == "reset" {
				g.Reset(7)
			} else {
				g.Close()
			}
			if len(w.carveImages) != 0 || len(g.world.carveImages) != 0 {
				t.Fatal("discarded world retained queued texture work")
			}
		})
	}
}

func TestCarvePlantsAcrossSectionSeamsAndReload(t *testing.T) {
	data := []terrain.SectionData{
		{ID: 0, Foreground: RockGrid{terrainRect(.1, -.1, .8, .2)}, Mushrooms: []MushroomGroup{{Mushrooms: []Mushroom{testGroundedMushroom(.4, -.1)}}}},
		{ID: 1, Foreground: RockGrid{terrainRect(.1, .9, .8, .2)}, Vines: []Vine{{Parent: -1, Points: []VinePoint{{P: V{X: .2, Y: 1}, Radius: .003}, {P: V{X: .8, Y: 1}, Radius: .003}}}}},
	}
	scene := queryScene(data...)
	if _, err := scene.CarveCircle(V{X: .4, Y: -1.1}, .02); err != nil {
		t.Fatal(err)
	}
	if len(scene.world.sections[0].geometry.Vegetation.Mushrooms) != 0 {
		t.Fatal("padded mushroom retained support across a seam")
	}
	if _, err := scene.CarveSegment(V{X: .5, Y: -1.05}, V{X: .5, Y: -.95}, .04); err != nil {
		t.Fatal(err)
	}
	if len(scene.world.sections[1].geometry.Vegetation.Vines) != 1 {
		t.Fatal("texture carve changed a neighbor-owned vine recipe")
	}
	for _, d := range data {
		want := scene.world.sections[d.ID].geometry.Vegetation
		mesh := render.PrepareSection(d, ViewShaded)
		mesh.Geometry = terrain.PrepareTerrainGeometry(d, 0)
		render.FinishSectionMesh(&mesh)
		scene.applyStoredCuts(&mesh)
		if !reflect.DeepEqual(want, mesh.Geometry.Vegetation) {
			t.Fatal("reloading restored unsupported mushrooms or changed vine recipes")
		}
	}
}

func TestCarvePlantsDuringEveryUploadStage(t *testing.T) {
	data := terrain.SectionData{Foreground: RockGrid{terrainRect(.1, .4, .8, .3)}, Mushrooms: []MushroomGroup{{Mushrooms: []Mushroom{testGroundedMushroom(.5, .4)}}},
		Vines: []Vine{{Parent: -1, Points: []VinePoint{{P: V{X: .1, Y: .4}, Radius: .003}, {P: V{X: .9, Y: .4}, Radius: .003}}}}}
	for stage := 0; stage <= uploadPlantsPublish; stage++ {
		t.Run(strconv.Itoa(stage), func(t *testing.T) {
			scene := queryScene()
			mesh := render.PrepareSection(data, ViewShaded)
			mesh.Geometry = terrain.PrepareTerrainGeometry(data, 0)
			render.FinishSectionMesh(&mesh)
			u := &sectionUpload{data: mesh, stage: stage, next: 3, initialized: true, needed: render.AllLayers}
			if stage >= uploadForegroundOutlines && stage <= uploadForegroundPublish {
				u.foreground = ebiten.NewImage(1, 1)
			}
			if stage >= uploadGrass && stage <= uploadForegroundPublish {
				u.grass = ebiten.NewImage(1, 1)
			}
			if stage >= uploadVines {
				u.vines, u.mushrooms, u.foregroundVines = ebiten.NewImage(1, 1), ebiten.NewImage(1, 1), ebiten.NewImage(1, 1)
				scene.world.sections[0] = &worldSection{geometry: mesh.Geometry, vegetationPending: true}
			}
			scene.world.upload = u
			if _, err := scene.CarveCircle(V{X: .5, Y: -.6}, .02); err != nil {
				t.Fatal(err)
			}
			scene.flushCarveImages()
			plants := u.data.Geometry.Vegetation
			if len(plants.Mushrooms) != 0 || len(plants.Vines) != 1 {
				t.Fatal("upload geometry retained damaged plants")
			}
			for _, a := range u.data.Plants {
				if !a.Dead {
					t.Fatal("damaged mushroom instance stayed alive")
				}
			}
			if !reflect.DeepEqual(u.data.Vines, mesh.Vines) || u.data.VinesBounds != mesh.VinesBounds || u.data.MushroomsBounds != mesh.MushroomsBounds {
				t.Fatal("cut regenerated recipes or changed their crop origin")
			}
			if u.stage != stage || u.next != 3 {
				t.Fatal("cut restarted unrelated upload work")
			}
			if len(u.data.VisualCuts) != 1 {
				t.Fatal("upload lost its final cut mask")
			}
			if stage >= uploadVines && !reflect.DeepEqual(scene.world.sections[0].geometry.Vegetation, plants) {
				t.Fatal("pending/published plants disagree")
			}
			u.deallocate()
		})
	}
}

func TestCarvePlantCropBoundsRemainStableThroughReplay(t *testing.T) {
	data := terrain.SectionData{Vines: []Vine{{Parent: -1, Points: []VinePoint{{P: V{X: .1, Y: .5}, Radius: .004}, {P: V{X: .9, Y: .5}, Radius: .004}}}}}
	scene := queryScene(data)
	if _, err := scene.CarveCircle(V{X: .5, Y: -.5}, .1); err != nil {
		t.Fatal(err)
	}
	first := render.PrepareSection(data, ViewShaded)
	first.Geometry = terrain.PrepareTerrainGeometry(data, 0)
	render.FinishSectionMesh(&first)
	scene.applyStoredCuts(&first)
	for replay := 0; replay < 2; replay++ {
		mesh := render.PrepareSection(data, ViewShaded)
		mesh.Geometry = terrain.PrepareTerrainGeometry(data, 0)
		render.FinishSectionMesh(&mesh)
		scene.applyStoredCuts(&mesh)
		if mesh.VinesBounds == (image.Rectangle{}) || mesh.VinesBounds != first.VinesBounds || !reflect.DeepEqual(mesh.Vines, first.Vines) {
			t.Fatal("replay double-cropped vegetation or shifted its raster origin")
		}
	}
}

func TestAuthoredAndRuntimeHolesProduceTheSamePlants(t *testing.T) {
	content := SectionContent{Guides: []Guide{{Pts: []V{{X: .15, Y: .4}, {X: .5, Y: .35}, {X: .85, Y: .45}}, Seed: 1234}}}
	load := func(id int64) SectionContent {
		if id == 0 {
			return content
		}
		return SectionContent{}
	}
	base := terrain.NewSectionBuilder(42, load).Build(0)
	if len(base.Mushrooms) == 0 || len(base.Vines) == 0 || len(base.ForegroundVines) == 0 {
		t.Fatal("test needs generated mushrooms and both vine layers")
	}
	content.Holes = []Hole{{Shape: HoleCircle, Center: base.Mushrooms[0].Mushrooms[0].Anchor, Radius: .024}}
	for _, vines := range [][]Vine{base.Vines, base.ForegroundVines} {
		vine := vines[0]
		point := vine.Points[len(vine.Points)/2].P
		content.Holes = append(content.Holes, Hole{Shape: HoleCircle, Center: point, Radius: .02})
	}
	scene := queryScene(base)
	for _, hole := range content.Holes {
		if _, err := scene.Carve(terrain.TranslateHoleY(hole, -1)); err != nil {
			t.Fatal(err)
		}
	}
	want := scene.world.sections[0].geometry.Vegetation
	authored := terrain.NewSectionBuilder(42, load).Build(0)
	if got := terrain.SectionVegetation(authored); !reflect.DeepEqual(got, want) {
		t.Fatal("authored holes regrew or reshuffled vegetation instead of applying the same damage")
	}
	// Public section geometry carries original vine recipes and supported plants.
	section, err := GenerateSectionWithConfig(Config{Seed: 42, MushroomDensity: terrain.DefaultMushroomDensity, LoadSection: load}, 0)
	if err != nil || !reflect.DeepEqual(section.Vines, want.Vines) || !reflect.DeepEqual(section.ForegroundVines, want.ForegroundVines) || !reflect.DeepEqual(section.Mushrooms, want.Mushrooms) {
		t.Fatalf("public authored section lost vegetation edits: %v", err)
	}
}

func TestCarveDisplayedPlantUsesItsOwnRecipeDuringReplacement(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	prepare := func(x float64) render.SectionMesh {
		data := terrain.SectionData{Foreground: RockGrid{terrainRect(.1, .4, .8, .3)},
			Mushrooms: []MushroomGroup{{Mushrooms: []Mushroom{testGroundedMushroom(x, .4)}}}}
		mesh := render.PrepareVegetationParts(data, ViewShaded, terrain.MushroomsPart)
		mesh.Geometry = terrain.PrepareTerrainGeometry(data, 0)
		return mesh
	}
	old, latest := prepare(.4), prepare(.7)
	if len(old.Plants) == 0 {
		t.Fatal("fixture has no displayed mushroom")
	}
	w.sections[0].deallocate()
	w.sections[0] = &worldSection{}
	u := &sectionUpload{data: old, raster: render.ScaleVegetationMesh(old, 32), pixels: 32, mushrooms: ebiten.NewImage(1, 1)}
	w.publishPlant(g, u, mushroomsProduct)
	s := w.sections[0]
	// A newer query recipe arrives while the old-width image is still shown.
	s.mesh, s.geometry = &latest, latest.Geometry
	if _, err := g.CarveCircle(V{X: .4, Y: -.6}, .02); err != nil {
		t.Fatal(err)
	}
	g.flushCarveImages()
	if s.mushrooms != nil || render.HasLiveAttachments(s.plants[mushroomsProduct].source.Plants, false) {
		t.Fatal("cut ignored the displayed fallback's own mushroom support")
	}
	if len(s.geometry.Vegetation.Mushrooms) != 1 || old.Plants[0].Dead {
		t.Fatal("fallback carving changed the newer placement or immutable source")
	}
}

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
	checkBatBurnPixels(g.t)
	checkPartialVegetationCarvePixels(g.t)
	checkIndependentGrassPixels(g.t)
	checkGrassEdgePixels(g.t)
	checkGrowthCompositionPixels(g.t)
	checkCarveCachedToothWallPixels(g.t)
	checkAnchorSupportedGrowthPixels(g.t)
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
		basePixels := readPixels(s.carveBase)
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
		if !bytes.Equal(basePixels, readPixels(s.carveBase)) {
			g.t.Fatal("overlapping cuts modified the immutable rock cache")
		}
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

func checkCarveCachedToothWallPixels(t *testing.T) {
	t.Helper()
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		for _, aa := range []AntialiasingMode{AntialiasingFull, AntialiasingOff, AntialiasingFXAA} {
			g, err := NewScene(Config{DisableBackground: true, Orientation: orientation, Antialiasing: aa})
			if err != nil {
				t.Fatal(err)
			}
			defer g.Close()
			g.SetRenderWidth(256)
			data := terrain.SectionData{Foreground: RockGrid{terrainRect(.2, .4, .6, .4)}}
			mesh := render.PrepareForeground(data, ViewShaded)
			mesh.Geometry = terrain.PrepareTerrainGeometry(data, 0)
			// A projected tooth wall extends beyond both its support and the cut.
			wall := render.TriangleMesh{Indices: []uint32{0, 1, 2, 0, 2, 3}}
			for _, p := range []V{{X: .4, Y: .3}, {X: .5, Y: .3}, {X: .5, Y: .35}, {X: .4, Y: .35}} {
				wall.Vertices = append(wall.Vertices, ebiten.Vertex{
					DstX: float32(p.X * render.RasterPixelsPerUnit), DstY: float32((p.Y - terrain.GenerationMinY) * render.RasterPixelsPerUnit),
					SrcX: .5, SrcY: .5, ColorR: 1, ColorA: 1,
				})
			}
			mesh.Foreground.Growth = append(mesh.Foreground.Growth, render.Attachment{
				Kind: render.EdgeAttachment, Underlay: true, Mesh: wall,
				Min: V{X: .4, Y: .3}, Max: V{X: .5, Y: .35}, Anchors: []V{{X: .45, Y: .41}},
			})
			mesh.Foreground.GrowthIndex = render.AttachmentIndex(mesh.Foreground.Growth)
			render.FinishSectionMesh(&mesh)
			g.world.startUpload(mesh)
			for i := 0; i < 1000 && g.world.upload != nil; i++ {
				g.world.receive(g)
			}
			if g.world.upload != nil {
				t.Fatal("tooth wall upload stalled")
			}
			s := g.world.sections[0]
			alpha := func() uint8 { return color.RGBAModel.Convert(s.foreground.At(115, 82)).(color.RGBA).A }
			if alpha() == 0 {
				t.Fatal("fixture did not draw the projected wall")
			}
			for _, point := range []V{{X: .45, Y: -.59}, {X: .455, Y: -.59}} {
				if _, err := g.CarveTerrain(Hole{Shape: HoleCircle, Center: terrain.WorldPoint(orientation, point), Radius: .01}); err != nil {
					t.Fatal(err)
				}
				g.flushCarveImages()
				if alpha() != 0 {
					t.Fatalf("%v/AA=%v: cached replay restored an unsupported tooth wall outside the cut", orientation, aa)
				}
			}
		}
	}
}

func checkAnchorSupportedGrowthPixels(t *testing.T) {
	t.Helper()
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		for _, aa := range []AntialiasingMode{AntialiasingFull, AntialiasingOff, AntialiasingFXAA} {
			for _, duringUpload := range []bool{false, true} {
				g, err := NewScene(Config{DisableBackground: true, DecorationBurn: true, Orientation: orientation, Antialiasing: aa})
				if err != nil {
					t.Fatal(err)
				}
				g.world.close()
				w := &world{sections: make(map[int64]*worldSection), done: make(chan struct{}), pixels: 256, disableBackground: true, antialiasing: aa}
				g.world = w
				data := terrain.SectionData{Foreground: RockGrid{terrainRect(.05, .05, .9, .9)}}
				mesh := render.PrepareForeground(data, ViewShaded, render.RockGrowthOptions{})
				mesh.Geometry = terrain.PrepareTerrainGeometry(data, 0)
				anchors := []V{{X: .18, Y: .45}, {X: .36, Y: .45}}
				for i, kind := range []render.AttachmentKind{render.GrassAttachment, render.BackgroundGrassAttachment, render.HangingAttachment, render.HangingAttachment} {
					x := .12 + .18*float64(i)
					lo, hi := V{X: x, Y: .2}, V{X: x + .12, Y: .35}
					supports := anchors
					if kind == render.HangingAttachment {
						supports = []V{{X: x + .06, Y: .45}}
					}
					m := render.TriangleMesh{Indices: []uint32{0, 1, 2, 0, 2, 3}}
					for _, p := range []V{lo, {X: hi.X, Y: lo.Y}, hi, {X: lo.X, Y: hi.Y}} {
						m.Vertices = append(m.Vertices, ebiten.Vertex{DstX: float32(p.X * render.RasterPixelsPerUnit), DstY: float32((p.Y - terrain.GenerationMinY) * render.RasterPixelsPerUnit), SrcX: .5, SrcY: .5, ColorR: .4, ColorG: .6, ColorB: .3, ColorA: 1})
					}
					mesh.Foreground.Growth = append(mesh.Foreground.Growth, render.Attachment{Kind: kind, Underlay: i == 1 || i == 2, Mesh: m, Min: lo, Max: hi, Anchors: supports})
				}
				mesh.Foreground.GrowthIndex = render.AttachmentIndex(mesh.Foreground.Growth)
				finish := func() {
					for tick := 0; tick < 1000 && w.upload != nil; tick++ {
						receiveTestBudget(g)
					}
					if w.upload != nil {
						t.Fatal("anchor fixture upload stalled")
					}
				}
				carve := func(p V, radius float64) {
					if _, err := g.CarveCircle(terrain.WorldPoint(orientation, p.Add(V{Y: terrain.SectionTop(0)})), radius); err != nil {
						t.Fatal(err)
					}
					g.flushCarveImages()
				}
				check := func(grass, lichen bool) {
					t.Helper()
					s := w.sections[0]
					for i, layer := range []*ebiten.Image{s.grass, s.growth[underGrowth].image, s.growth[underGrowth].image, s.growth[surfaceGrowth].image} {
						want := grass
						if i >= 2 {
							want = lichen
						}
						visible := false
						if layer != nil {
							x, y := int((.18+.18*float64(i))*float64(w.pixels)), int(.25*float64(w.pixels))
							visible = color.RGBAModel.Convert(layer.At(x, y)).(color.RGBA).A > 0
						}
						if visible != want {
							t.Fatalf("%v/AA=%v/upload=%v/width=%d: growth %d visible=%v, want %v", orientation, aa, duringUpload, w.pixels, i, visible, want)
						}
					}
				}
				w.startUpload(mesh)
				if !duringUpload {
					finish()
					check(true, true)
				} else {
					budget := uploadBudget{start: time.Now().Add(time.Hour), draws: uploadDrawsPerTick - 1}
					w.receiveBudget(g, &budget)
					if w.upload == nil {
						t.Fatal("fixture finished before the pending-upload cut")
					}
				}
				// Hit each plant's artwork while missing every support.
				for i := 0; i < 4; i++ {
					carve(V{X: .18 + .18*float64(i), Y: .25}, .025)
				}
				finish()
				check(true, true)
				burn := ebiten.NewImage(256, 256)
				g.decorationBurn.Draw(burn, -1, render.RenderTransform{Pixels: 256}, false)
				g.decorationBurn.Draw(burn, -1, render.RenderTransform{Pixels: 256}, true)
				burnPixels := make([]byte, 4*256*256)
				burn.ReadPixels(burnPixels)
				burn.Deallocate()
				for _, value := range burnPixels {
					if value != 0 {
						t.Fatal("an anchor miss burned supported growth")
					}
				}
				g.SetRenderWidth(384)
				w.startUpload(mesh)
				finish()
				check(true, true)
				carve(anchors[0], .005)
				check(false, true)
				carve(V{X: .54, Y: .45}, .005)
				carve(V{X: .72, Y: .45}, .005)
				check(false, false)
				w.sections[0].deallocate()
				delete(w.sections, 0)
				w.startUpload(mesh)
				finish()
				check(false, false)
				g.Close()
			}
		}
	}
}

func carvePixelFixture() terrain.SectionData {
	cell := terrainRect(.05, .35, .90, .55)
	// Keep foreground tufts in this colony with both smooth and rough edges,
	// so upload/readback tests exercise the separate grass image.
	cell.Center.X -= .015
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

func TestDirtyTilesMergeDenseRegionsWithoutFillingGaps(t *testing.T) {
	dst := ebiten.NewImage(320, 256)
	defer dst.Deallocate()
	bounds := image.Rect(0, 320, 320, 640)
	region := func(r image.Rectangle) geom.Bounds {
		return geom.Bounds{Min: V{X: float64(r.Min.X) / 320, Y: float64(r.Min.Y) / 320}, Max: V{X: float64(r.Max.X) / 320, Y: float64(r.Max.Y) / 320}}
	}
	for _, tc := range []struct {
		name    string
		regions []geom.Bounds
		want    []image.Rectangle
	}{
		{"dense", []geom.Bounds{region(image.Rect(1, 1, 250, 190))}, []image.Rectangle{image.Rect(0, 0, 256, 192)}},
		{"gap", []geom.Bounds{region(image.Rect(1, 1, 63, 127)), region(image.Rect(193, 1, 255, 127))}, []image.Rectangle{image.Rect(0, 0, 64, 128), image.Rect(192, 0, 256, 128)}},
		{"step", []geom.Bounds{region(image.Rect(1, 1, 127, 127)), region(image.Rect(65, 129, 127, 190))}, []image.Rectangle{image.Rect(0, 0, 128, 128), image.Rect(64, 128, 128, 192)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := dirtyTiles(dst, bounds, -1, 320, tc.regions)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d replay rectangles, want %d", len(got), len(tc.want))
			}
			for i, dirty := range got {
				if r := dirtyRaster(bounds, -1, 320, dirty, dst); r != tc.want[i] {
					t.Fatalf("replay rectangle %d = %v, want %v", i, r, tc.want[i])
				}
			}
		})
	}
}
