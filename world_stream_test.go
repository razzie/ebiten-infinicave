package infinicave

import (
	"fmt"
	"image"
	"image/color"
	"reflect"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestGrassAndDecorationCachesPublishResizeAndCarveTogether(t *testing.T) {
	g, err := NewScene(Config{GrassDensity: 1, MushroomDensity: 1, SpiderWebDensity: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.Close)
	cell := terrainRect(.15, .3, .7, .4)
	cell.Normal, cell.Color = V3{Y: -.8, Z: .6}, color.NRGBA{R: 140, G: 120, B: 90, A: 255}
	cell.Z, cell.Ambient, cell.Shadow = .04, 1, 1
	topology := terrain.NewRockTopology(RockGrid{cell})
	topology.Appearance = topology.Grid
	data := terrain.SectionData{Foreground: topology.Grid, ForegroundTopology: topology,
		Guides: []Guide{terrain.SplineGuide([]V{{X: .14, Y: .3}, {X: .86, Y: .3}}, 42)}}
	for i := 0; i < 40; i++ {
		x := .2 + float64(i)*.015
		data.Mushrooms = append(data.Mushrooms, MushroomGroup{Mushrooms: []Mushroom{testGroundedMushroom(x, .3)}})
		topology.SpiderWebs = append(topology.SpiderWebs, terrain.NewSpiderWeb([]V{{X: x, Y: .31}, {X: x + .06, Y: .31}, {X: x + .03, Y: .36}}, .5))
	}
	mesh := render.PrepareSection(data, ViewShaded, render.RockGrowthOptions{Grass: 1})
	mesh.Geometry = terrain.PrepareTerrainGeometry(data, 0)
	render.FinishSectionMesh(&mesh)
	if len(mesh.Foreground.Growth) == 0 || len(mesh.Mushrooms.Indices) == 0 || len(mesh.SpiderWebs.Indices) == 0 {
		t.Fatal("fixture must supply grass, mushrooms, and webs")
	}
	w := g.world
	w.startUpload(mesh)
	finish := func() {
		t.Helper()
		for tick := 0; tick < 80 && w.upload != nil; tick++ {
			w.receive(g)
		}
		if w.upload != nil {
			t.Fatal("decoration upload did not complete")
		}
	}
	finish()
	s := w.sections[0]
	if s == nil || s.grass == nil || s.mushrooms == nil || s.spiderWebs == nil {
		t.Fatal("publication lost a decoration cache")
	}
	before := s.grass
	g.SetRenderWidth(501)
	w.startUpload(*s.mesh)
	finish()
	if s.grass == before || s.renderWidth() != 501 || s.vegetationWidth() != 501 || s.grass.Bounds().Dx() != 501 {
		t.Fatal("resize retained obsolete grass caches")
	}
	if s.mushrooms == nil || s.spiderWebs == nil || s.spiderWebsBounds.Min.Y < 501 || s.spiderWebsBounds.Max.Y > 1002 {
		t.Fatal("resize lost decorations or web ownership")
	}
	if _, err := g.CarveCircle(V{X: .5, Y: -.5}, 1); err != nil {
		t.Fatal(err)
	}
	g.flushCarveImages()
	for _, list := range [][]render.Attachment{s.mesh.Foreground.Growth, s.mesh.Plants, s.mesh.Webs} {
		for _, a := range list {
			if a.Kind != render.SurfaceMoss && len(a.Anchors) > 0 && !a.Dead {
				t.Fatal("carving left a live unsupported attachment")
			}
		}
	}
}

func TestResizeUsesNativeImagesAndDefersOffscreenSections(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	viewport := Viewport{Y: -.8, Height: .8}
	before := w.sections[1].terrain
	geometry := w.sections[0].geometry
	hit, err := g.Query(Ray{Origin: V{X: .5, Y: -.5}}, QueryOptions{Targets: TargetRock})
	if err != nil || !hit.Found {
		t.Fatalf("initial rock query: %+v, %v", hit, err)
	}
	g.SetRenderWidth(1920)
	finishResolutionRefresh(t, g, viewport)
	if w.sections[0].terrain.Bounds() != image.Rect(0, 0, 3840, 1920) || w.sections[0].foreground.Bounds() != image.Rect(0, 0, 1920, 1920) {
		t.Fatal("visible terrain did not use native raster dimensions")
	}
	if w.sections[1].terrain != before || w.sections[1].renderWidth() != 1000 {
		t.Fatal("offscreen terrain was rerendered during resize")
	}
	if w.sections[0].geometry != geometry || len(w.jobs) != 0 {
		t.Fatal("resize regenerated CPU terrain")
	}
	if _, available := g.Formation(hit.Hit.FormationID); !available {
		t.Fatal("resize invalidated formation IDs")
	}
	finishResolutionRefresh(t, g, Viewport{Y: -1.8, Height: .8})
	if w.sections[1].renderWidth() != 1920 || w.sections[1].terrain.Bounds().Dx() != 3840 {
		t.Fatal("scrolling did not refresh newly visible terrain")
	}
}

func TestResizeKeepsCutsAndCoalescesPartialUploads(t *testing.T) {
	g := resolutionTestScene(t)
	viewport := Viewport{Y: -.8, Height: .8}
	if _, err := g.CarveCircle(V{X: .5, Y: -.5}, .1); err != nil {
		t.Fatal(err)
	}
	geometry := g.world.sections[0].geometry
	// Force a partial refresh even though cheap stages now share a tick.
	g.world.sections[0].mesh.Foreground.Outlines = append(g.world.sections[0].mesh.Foreground.Outlines,
		make([]render.TriangleMesh, uploadDrawsPerTick*2+1)...)
	g.SetRenderWidth(1600)
	g.Update(viewport)
	g.Update(viewport) // Allocate the first native terrain image.
	if g.world.upload == nil || g.world.upload.foreground == nil {
		t.Fatal("resize did not begin a refresh")
	}
	g.SetRenderWidth(1920)
	finishResolutionRefresh(t, g, viewport)
	if g.world.sections[0].geometry != geometry || len(g.world.cuts) != 1 || g.world.sections[0].terrain.Bounds().Dx() != 3840 {
		t.Fatal("rapid resize lost cuts or retained an obsolete resolution")
	}
	hit, err := g.Query(Ray{Origin: V{X: .5, Y: -.5}}, QueryOptions{Targets: TargetRock})
	if err != nil || hit.Found || !hit.Complete {
		t.Fatalf("resized scene refilled the carved hole: %+v, %v", hit, err)
	}
	g.Reset(42)
	if g.world.renderWidth() != 1920 {
		t.Fatal("seed reset lost the native render resolution")
	}
}

func TestResizeRefreshesNeighborOnlyForVisibleVegetation(t *testing.T) {
	g := resolutionTestScene(t)
	neighbor := g.world.sections[1]
	// Section 1's padded window begins at -3; this vine reaches into [-.9, -.7].
	neighbor.mesh.VinesBounds = image.Rect(100, 2100, 110, 2300)
	g.SetRenderWidth(1500)
	finishResolutionRefresh(t, g, Viewport{Y: -.8, Height: .8})
	if g.world.sections[1].vegetationWidth() != 1500 || g.world.sections[1].renderWidth() != 1000 || g.world.sections[2].renderWidth() != 1000 {
		t.Fatal("resize did not restrict neighbor refreshes to contributing vegetation")
	}
}

func TestNeighborPlantsDeferRockUploadUntilScrolling(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	source := *w.sections[1].mesh
	source.VinesBounds = image.Rect(100, 2100, 110, 2300)
	geometry := source.Geometry
	w.sections[1].deallocate()
	w.sections[1] = &worldSection{mesh: &source, geometry: geometry, vegetationPending: true}
	finishResolutionRefresh(t, g, Viewport{Y: -.8, Height: .8})
	s := w.sections[1]
	if s.foreground != nil || s.terrain != nil || s.vegetationPending || s.mesh == nil {
		t.Fatal("neighbor plants required offscreen rock images or lost their recipes")
	}
	finishResolutionRefresh(t, g, Viewport{Y: -1.8, Height: .8})
	s = w.sections[1]
	if s.foreground == nil || s.terrain == nil || s.geometry != geometry {
		t.Fatal("scrolling failed to upload retained neighbor rock recipes")
	}
}

func TestLookaheadUploadsAllLayersBeforeExposure(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		for _, velocity := range []float64{0, -.00001, .00001} {
			for _, disableBackground := range []bool{false, true} {
				t.Run(fmt.Sprintf("%v/velocity=%g/background-disabled=%v", orientation, velocity, disableBackground), func(t *testing.T) {
					g := resolutionTestScene(t)
					g.orientation = orientation
					w := g.world
					for id, s := range w.sections {
						s.deallocate()
						delete(w.sections, id)
					}
					w.lookaheadSections, w.disableBackground = 1, disableBackground
					w.pixels = 32
					for _, id := range []int64{9, 10, 11} {
						w.sections[id] = &worldSection{}
					}
					viewport := Viewport{Y: -11, Height: 1, Velocity: velocity}
					if orientation == Horizontal {
						viewport = Viewport{X: 10, Width: 1, Velocity: -velocity}
					}
					target := int64(11) // stationary neighbor
					if velocity < 0 {
						target = 12
					} else if velocity > 0 {
						target = 8
					}
					mesh := resolutionTestMesh(target)
					bounds := image.Rect(100, 1200, 200, 1400)
					mesh.Vines, mesh.ForegroundVines = []render.TriangleMesh{{}}, []render.TriangleMesh{{}}
					mesh.VinesBounds, mesh.ForegroundVinesBounds = bounds, bounds
					mesh.MushroomsBounds, mesh.SpiderWebsBounds = bounds, bounds
					mesh.Foreground.Grass = mesh.Foreground.Faces
					v, _ := orientedViewport(orientation, viewport)
					if meshVisible(mesh, v) {
						t.Fatal("fixture must stay entirely outside the viewport")
					}
					w.savePending(mesh)
					finish := func() *worldSection {
						t.Helper()
						for tick := 0; tick < 100; tick++ {
							if !g.Update(viewport) {
								t.Fatal("offscreen uploads delayed a ready viewport")
							}
							s := w.sections[target]
							if s != nil && !s.vegetationPending && !w.needsVisibleRefresh(target, s) && w.upload == nil {
								return s
							}
						}
						t.Fatal("lookahead never finished its offscreen layers")
						return nil
					}
					s := finish()
					if s.foreground == nil || s.grass == nil || s.foregroundVines == nil || s.mushrooms == nil || s.spiderWebs == nil ||
						(s.terrain == nil) != disableBackground || (s.vines == nil) != disableBackground {
						t.Fatal("lookahead left an offscreen layer unprepared")
					}
					// Resize refreshes the same offscreen recipes without regeneration.
					g.SetRenderWidth(48)
					s = finish()
					foreground, vines, mushrooms, webs := s.foreground, s.foregroundVines, s.mushrooms, s.spiderWebs
					for _, id := range []int64{target - 1, target + 1} {
						if w.sections[id] == nil {
							w.sections[id] = &worldSection{}
						}
					}
					viewport = Viewport{Y: -float64(target + 1), Height: 1}
					if orientation == Horizontal {
						viewport = Viewport{X: float64(target), Width: 1}
					}
					if !g.Update(viewport) || w.upload != nil || s.foreground != foreground || s.foregroundVines != vines || s.mushrooms != mushrooms || s.spiderWebs != webs || len(w.jobs) != 0 {
						t.Fatal("entering a prefetched section restarted uploads or generation")
					}
				})
			}
		}
	}
}

func TestVisibleRefreshPreemptsLookaheadVegetation(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.lookaheadSections = 1
	w.viewport = Viewport{Y: -1, Height: 1, Velocity: -.00001}
	plants := resolutionTestMesh(2)
	plants.Layers = render.VegetationLayer
	plants.Vines = make([]render.TriangleMesh, uploadDrawsPerTick*8+1)
	plants.VinesBounds = image.Rect(100, 1200, 200, 1400)
	w.startUpload(plants)
	w.receive(g)
	paused := w.upload
	if paused == nil || paused.plants[backgroundVinesProduct].cursor.next == 0 {
		t.Fatal("offscreen vegetation did not begin uploading")
	}
	next := paused.plants[backgroundVinesProduct].cursor.next
	w.sections[0].foreground.Deallocate()
	w.sections[0].foreground = nil
	g.Update(w.viewport)
	if w.sections[0].foreground == nil && w.parked[0] == nil && (w.upload == nil || w.upload.data.ID != 0) {
		t.Fatal("visible cached terrain was not admitted ahead of lookahead completion")
	}
	for tick := 0; tick < 20 && w.upload != nil; tick++ {
		w.receive(g)
	}
	w.receive(g)
	if paused.plants[backgroundVinesProduct].cursor.next < next {
		t.Fatal("lookahead vegetation restarted instead of resuming")
	}
}

func TestResizeRefreshFollowsCameraAndRetainsCutsDuringUpload(t *testing.T) {
	g := resolutionTestScene(t)
	for _, id := range []int64{0, 1} {
		g.world.sections[id].mesh.Vines = make([]render.TriangleMesh, uploadDrawsPerTick*8)
		g.world.sections[id].mesh.VinesBounds = image.Rect(100, 1200, 200, 1400)
		g.world.sections[id].mesh.Foreground.Outlines = append(g.world.sections[id].mesh.Foreground.Outlines,
			make([]render.TriangleMesh, uploadDrawsPerTick*2+1)...)
	}
	g.SetRenderWidth(1920)
	g.Update(Viewport{Y: -.8, Height: .8})
	g.Update(Viewport{Y: -.8, Height: .8})
	g.Update(Viewport{Y: -1.8, Height: .8})
	if g.world.upload == nil || g.world.upload.data.ID != 1 || g.world.sections[0].renderWidth() != 1000 {
		t.Fatal("moving the camera did not defer the offscreen refresh")
	}
	viewport := Viewport{Y: -1.8, Height: .8}
	for tick := 0; tick < 30 && g.world.sections[1].renderWidth() != 1920; tick++ {
		g.Update(viewport)
	}
	if g.world.sections[1].renderWidth() != 1920 || g.world.upload == nil {
		t.Fatal("fixture did not leave an independent vegetation upload after terrain publication")
	}
	if _, err := g.CarveCircle(V{X: .5, Y: -1.5}, .1); err != nil {
		t.Fatal(err)
	}
	finishResolutionRefresh(t, g, viewport)
	g.SetRenderWidth(1600)
	finishResolutionRefresh(t, g, viewport)
	// Retained rendering triangles must keep the hole as well as query geometry.
	mesh := g.world.sections[1].mesh.Foreground.Faces
	center := V{X: 500, Y: 1500}
	for i := 0; i < len(mesh.Indices); i += 3 {
		var triangle []V
		for _, index := range mesh.Indices[i : i+3] {
			v := mesh.Vertices[index]
			triangle = append(triangle, V{X: float64(v.DstX), Y: float64(v.DstY)})
		}
		if geom.InsidePolygon(center, triangle) {
			t.Fatal("a cut during upload was lost from the retained rendering mesh")
		}
	}
}

func TestDisabledBackgroundSkipsUploadsAndRefresh(t *testing.T) {
	g, err := NewScene(Config{DisableBackground: true, View: ViewClay})
	if err != nil {
		t.Fatal(err)
	}
	g.world.close()
	w := &world{disableBackground: true, sections: make(map[int64]*worldSection), done: make(chan struct{})}
	g.world = w
	t.Cleanup(g.Close)
	mesh := resolutionTestMesh(0)
	mesh.Vines = []render.TriangleMesh{{}}
	mesh.ForegroundVines = []render.TriangleMesh{{}}
	mesh.VinesBounds = image.Rect(10, 1000, 30, 1020)
	mesh.ForegroundVinesBounds = mesh.VinesBounds
	for _, pixels := range []int{64, 128} {
		w.pixels = pixels
		w.startUpload(mesh)
		for tick := 0; tick < 100 && w.upload != nil; tick++ {
			w.receive(g)
			if w.upload != nil && (w.upload.img != nil || w.upload.vines != nil) {
				t.Fatal("disabled background allocated upload images")
			}
		}
		if w.upload != nil {
			t.Fatal("foreground upload did not complete")
		}
		s := w.sections[0]
		if s == nil || s.foreground == nil || s.foregroundVines == nil || s.vegetationPending {
			t.Fatal("disabled background prevented foreground publication")
		}
		if s.terrain != nil || s.vines != nil || w.needsVisibleRefresh(0, s) || w.BackgroundVisible(-1, 1) {
			t.Fatal("disabled background was uploaded or still requires refreshing")
		}
		if s.mesh.LayerMask() != render.AllLayers || len(s.mesh.Vines) == 0 {
			t.Fatal("disabling rendering changed retained CPU recipes")
		}
	}
}

func TestSectionUploadsPublishProductsIndependently(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.sections[0].deallocate()
	delete(w.sections, 0)
	w.pixels = 64
	bounds := image.Rect(10, 1000, 330, 1320)
	mesh := render.SectionMesh{ID: 0,
		Background:      render.GridMesh{Outlines: make([]render.TriangleMesh, uploadDrawsPerTick*8)},
		Vines:           make([]render.TriangleMesh, uploadDrawsPerTick*8),
		ForegroundVines: make([]render.TriangleMesh, uploadDrawsPerTick*8),
		VinesBounds:     bounds, ForegroundVinesBounds: bounds, MushroomsBounds: bounds,
	}
	w.savePending(mesh)
	var foreground, mushroom bool
	for tick := 0; tick < 100; tick++ {
		w.receive(g)
		s := w.sections[0]
		if s == nil {
			continue
		}
		if !foreground && s.foreground != nil {
			foreground = true
			if s.terrain != nil || !s.vegetationPending {
				t.Fatal("small rock product waited for slow products")
			}
		}
		if !mushroom && s.mushrooms != nil {
			mushroom = true
			if s.foregroundVines != nil || !s.vegetationPending {
				t.Fatal("mushrooms waited for unfinished vines")
			}
		}
		if w.upload != nil {
			if s.foreground != nil && w.upload.foreground != nil || s.mushrooms != nil && w.upload.mushrooms != nil {
				t.Fatal("staging retained a published image")
			}
			if w.ensure(-.8, .8, 0) {
				t.Fatal("partial products reported full readiness")
			}
			continue
		}
		if !foreground || !mushroom || s.vegetationPending || s.terrain == nil || s.vines == nil || s.foregroundVines == nil {
			t.Fatal("independent products did not all publish")
		}
		if s.vinesBounds.Empty() || s.vines.Bounds().Size() != s.vinesBounds.Size() {
			t.Fatal("publication lost crop bounds")
		}
		return
	}
	t.Fatal("independent uploads stalled")
}

func TestEarlyTerrainPublicationReusesImagesForVegetation(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.sections[0].deallocate()
	delete(w.sections, 0)
	w.terrainMeshes = make(chan sectionResult, 1)
	w.working = true
	early := resolutionTestMesh(0)
	early.TerrainOnly = true
	w.terrainMeshes <- sectionResult{SectionMesh: early}
	w.receive(g)
	for tick := 0; tick < 20 && w.upload != nil; tick++ {
		w.receive(g)
	}
	section := w.sections[0]
	if section == nil || !section.vegetationPending || w.upload != nil || !w.working {
		t.Fatal("terrain did not publish independently of unfinished generation")
	}
	if w.ensure(-.8, .8, 0) {
		t.Fatal("early terrain reported vegetation ready")
	}
	background, foreground := section.terrain, section.foreground
	complete := early
	complete.TerrainOnly = false
	geometry := *early.Geometry
	complete.Geometry = &geometry
	w.results <- sectionResult{SectionMesh: complete}
	w.receive(g)
	for tick := 0; tick < 20 && w.upload != nil; tick++ {
		w.receive(g)
	}
	if w.upload != nil || section.vegetationPending || section.geometry != &geometry {
		t.Fatal("complete geometry and vegetation did not publish")
	}
	if section.terrain != background || section.foreground != foreground {
		t.Fatal("vegetation completion redundantly uploaded terrain")
	}
}

func TestCollectResultsDuringUploadAndDiscardCanceledTerrain(t *testing.T) {
	w := &world{sections: make(map[int64]*worldSection), pending: make(map[int64]render.SectionMesh),
		results: make(chan sectionResult, 1), terrainMeshes: make(chan sectionResult, 1), canceled: make(chan sectionCancellation, 1),
		working: true, activeID: 3, upload: &sectionUpload{data: render.SectionMesh{ID: 1}}}
	w.results <- sectionResult{SectionMesh: render.SectionMesh{ID: 3}}
	w.collect()
	if w.working || w.pending[3].ID != 3 || w.upload.data.ID != 1 {
		t.Fatal("active upload blocked completed CPU work")
	}
	// A late early result must never replace an already collected full result.
	w.terrainMeshes <- sectionResult{SectionMesh: render.SectionMesh{ID: 3, TerrainOnly: true}}
	w.collect()
	if w.pending[3].TerrainOnly {
		t.Fatal("late terrain replaced a complete section")
	}
	w.working, w.activeID = true, 4
	w.terrainMeshes <- sectionResult{SectionMesh: render.SectionMesh{ID: 4, TerrainOnly: true}}
	w.canceled <- sectionCancellation{id: 4}
	w.collect()
	if _, ok := w.pending[4]; ok || w.working {
		t.Fatal("canceled provisional terrain survived collection")
	}
}

func TestCarveSurvivesEarlyTerrainAndVegetationCompletion(t *testing.T) {
	for _, afterPublication := range []bool{false, true} {
		g := resolutionTestScene(t)
		w := g.world
		w.sections[0].deallocate()
		delete(w.sections, 0)
		original := resolutionTestMesh(0)
		early := original
		early.TerrainOnly = true
		w.startUpload(early)
		if afterPublication {
			for tick := 0; tick < 20 && w.upload != nil; tick++ {
				w.receive(g)
			}
		}
		if _, err := g.CarveCircle(V{X: .5, Y: -.5}, .08); err != nil {
			t.Fatal(err)
		}
		for tick := 0; tick < 20 && w.upload != nil; tick++ {
			w.receive(g)
		}
		// The complete result still carries the worker's original immutable
		// geometry; persisted cuts must apply before vegetation can publish.
		w.results <- sectionResult{SectionMesh: original}
		w.receive(g)
		for tick := 0; tick < 20 && w.upload != nil; tick++ {
			w.receive(g)
		}
		section := w.sections[0]
		if section == nil || section.vegetationPending || section.geometry.Collision.Contains(V{X: .5, Y: -.5}) {
			t.Fatal("vegetation completion lost a cut to early terrain")
		}
		mesh := section.mesh.Foreground.Faces
		center := V{X: 500, Y: 1500}
		for i := 0; i < len(mesh.Indices); i += 3 {
			var triangle []V
			for _, index := range mesh.Indices[i : i+3] {
				v := mesh.Vertices[index]
				triangle = append(triangle, V{X: float64(v.DstX), Y: float64(v.DstY)})
			}
			if geom.InsidePolygon(center, triangle) {
				t.Fatal("completed rendering filled the early cut")
			}
		}
		g.Close()
	}
}

func TestIndependentLayersMergeWithoutReuploadingForeground(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.sections[0].deallocate()
	delete(w.sections, 0)
	mesh := resolutionTestMesh(0)
	foreground := mesh
	foreground.TerrainOnly = true
	foreground.Layers = render.ForegroundLayer
	w.startUpload(foreground)
	for tick := 0; tick < 20 && w.upload != nil; tick++ {
		w.receive(g)
	}
	s := w.sections[0]
	if s == nil || s.foreground == nil || s.terrain != nil || !s.vegetationPending {
		t.Fatal("foreground depends on another layer")
	}
	image := s.foreground
	revision := w.revision
	background := mesh
	background.TerrainOnly = true
	background.Layers = render.BackgroundLayer
	w.startUpload(background)
	for tick := 0; tick < 20 && w.upload != nil; tick++ {
		w.receive(g)
	}
	if s.terrain == nil || s.foreground != image || w.revision != revision {
		t.Fatal("background replaced foreground or query geometry")
	}
	web := terrain.NewSpiderWeb([]V{{X: .3, Y: .3}, {X: .36, Y: .3}, {X: .33, Y: .35}}, .5)
	plants := render.PrepareVegetation(terrain.SectionData{
		ForegroundTopology: &terrain.RockTopology{SpiderWebs: []terrain.SpiderWeb{web}},
	}, ViewShaded)
	plants.Geometry = mesh.Geometry
	w.startUpload(plants)
	for tick := 0; tick < 20 && w.upload != nil; tick++ {
		w.receive(g)
	}
	if s.foreground != image || s.spiderWebs == nil || s.vegetationPending || s.mesh.LayerMask() != render.AllLayers {
		t.Fatal("decoration completion lost an independent layer")
	}
	if !reflect.DeepEqual(s.mesh.SpiderWebs, plants.SpiderWebs) || s.mesh.SpiderWebsBounds != plants.SpiderWebsBounds {
		t.Fatal("decoration publication lost cached webs")
	}
	w.pixels = 501
	w.startUpload(*s.mesh)
	for tick := 0; tick < 20 && w.upload != nil; tick++ {
		w.receive(g)
	}
	if s.spiderWebs == nil || s.vegetationWidth() != 501 || s.spiderWebsBounds.Min.Y < 501 || s.spiderWebsBounds.Max.Y > 1002 {
		t.Fatal("resize lost the independent web layer or its band alignment")
	}
}

func TestRockUploadPreemptsAndResumesVegetation(t *testing.T) {
	for _, layer := range []render.MeshLayers{render.ForegroundLayer, render.BackgroundLayer} {
		t.Run(fmt.Sprint(layer), func(t *testing.T) {
			g := resolutionTestScene(t)
			w := g.world
			plants := resolutionTestMesh(0)
			plants.Layers = render.VegetationLayer
			plants.Vines = make([]render.TriangleMesh, uploadDrawsPerTick*8+1)
			plants.VinesBounds = image.Rect(10, 20, 30, 40)
			w.startUpload(plants)
			w.receive(g) // Start the vegetation phase.
			paused := w.upload
			if paused == nil || paused.plants[backgroundVinesProduct].cursor.next == 0 {
				t.Fatal("vegetation did not begin")
			}
			next := paused.plants[backgroundVinesProduct].cursor.next
			// A newly visible rock layer takes the upload slot at the batch boundary.
			if layer == render.ForegroundLayer {
				w.sections[1].foreground.Deallocate()
				w.sections[1].foreground = nil
			} else {
				w.sections[1].terrain.Deallocate()
				w.sections[1].terrain = nil
			}
			foreground := resolutionTestMesh(1)
			foreground.Layers = layer
			foreground.TerrainOnly = true
			w.savePending(foreground)
			w.receive(g)
			if w.parked[1] == nil && (w.upload == nil || w.upload.data.ID != 1) && ((layer == render.ForegroundLayer && w.sections[1].foreground == nil) || (layer == render.BackgroundLayer && w.sections[1].terrain == nil)) {
				t.Fatal("rock layer was not admitted ahead of vegetation completion")
			}
			for tick := 0; tick < 20 && w.upload != nil; tick++ {
				w.receive(g)
			}
			if (layer == render.ForegroundLayer && w.sections[1].foreground == nil) ||
				(layer == render.BackgroundLayer && w.sections[1].terrain == nil) {
				t.Fatal("rock layer did not publish")
			}
			w.receive(g)
			if paused.plants[backgroundVinesProduct].cursor.next < next {
				t.Fatal("vegetation restarted instead of resuming")
			}
			for tick := 0; tick < 20 && w.upload != nil; tick++ {
				w.receive(g)
			}
			if w.sections[0].vegetationPending || len(w.parked) != 0 {
				t.Fatal("paused vegetation did not finish")
			}
		})
	}
}

func TestResizeRetainsParkedSourceAndDropsPartialImages(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	u := &sectionUpload{data: resolutionTestMesh(0), stage: uploadMushrooms, initialized: true, pixels: 1000}
	u.surface = render.NewSectionImageAt(1, 10)
	w.parked = map[int64]*sectionUpload{0: u}
	g.SetRenderWidth(1200)
	if len(w.parked) != 0 || u.surface != nil || w.pending[0].ID != 0 {
		t.Fatal("resize lost parked CPU data or kept old GPU work")
	}
	finishResolutionRefresh(t, g, Viewport{Y: -.8, Height: .8})
	if w.sections[0].foreground.Bounds().Dx() != 1200 {
		t.Fatal("parked source did not refresh at the latest width")
	}
}

func TestVegetationLayersShareUploadBudget(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.sections[0].vegetationPending = true
	plants := resolutionTestMesh(0)
	plants.Layers = render.VegetationLayer
	plants.Vines = make([]render.TriangleMesh, uploadDrawsPerTick-1)
	plants.ForegroundVines = make([]render.TriangleMesh, uploadDrawsPerTick-1)
	plants.VinesBounds = image.Rect(10, 20, 30, 40)
	plants.ForegroundVinesBounds = plants.VinesBounds
	w.startUpload(plants)
	w.receive(g)
	if w.upload == nil || !w.sections[0].vegetationPending {
		t.Fatal("separate vegetation stages each consumed a full tick budget")
	}
	if w.upload.plants[backgroundVinesProduct].cursor.next+w.upload.plants[foregroundVinesProduct].cursor.next > uploadDrawsPerTick {
		t.Fatal("independent products each consumed a full tick budget")
	}
	for tick := 0; tick < 10 && w.upload != nil; tick++ {
		w.receive(g)
	}
	if w.upload != nil || w.sections[0].vegetationPending {
		t.Fatal("vegetation stopped making progress after exhausting its budget")
	}
}

func TestCheapUploadStagesPublishInSameTick(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.sections[0].deallocate()
	delete(w.sections, 0)
	w.pixels = 10
	w.startUpload(render.SectionMesh{ID: 0, Layers: render.ForegroundLayer, TerrainOnly: true})
	w.receive(g)
	s := w.sections[0]
	if s == nil || s.foreground == nil || s.terrain != nil || !s.vegetationPending {
		t.Fatal("empty face/outline stages delayed foreground publication")
	}
	if w.upload != nil && w.upload.foreground != nil {
		t.Fatal("publication retained ownership of the foreground image")
	}
}

func TestFaceUploadRetainsSurfaceAcrossIndexBudgets(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.sections[0].deallocate()
	delete(w.sections, 0)
	w.pixels = 10
	batch := (uploadIndicesPerTick / 3) * 3
	mesh := render.TriangleMesh{
		Vertices: []ebiten.Vertex{{DstX: 100, DstY: 1100, ColorA: 1}, {DstX: 900, DstY: 1100, ColorA: 1}, {DstX: 500, DstY: 1900, ColorA: 1}},
		Indices:  make([]uint32, batch*2+3),
	}
	for i := range mesh.Indices {
		mesh.Indices[i] = uint32(i % 3)
	}
	w.startUpload(render.SectionMesh{ID: 0, Layers: render.ForegroundLayer, TerrainOnly: true, Foreground: render.GridMesh{Faces: mesh}})
	w.receive(g)
	u := w.upload
	if u == nil || u.surface == nil || u.faceNext != batch || (w.sections[0] != nil && w.sections[0].foreground != nil) {
		t.Fatal("first face batch exceeded its index budget or published incomplete terrain")
	}
	surface := u.surface
	for tick := 0; tick < 10 && (w.sections[0] == nil || w.sections[0].foreground == nil); tick++ {
		before := u.faceNext
		w.receive(g)
		if u.surface != nil {
			if u.surface != surface || u.faceNext-before > batch {
				t.Fatal("partial face upload discarded its accumulator or exceeded the tick budget")
			}
		}
	}
	if s := w.sections[0]; s == nil || s.foreground == nil || u.surface != nil {
		t.Fatal("budgeted face batches did not resolve and publish their completed layer")
	}
}
