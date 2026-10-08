package infinicave

import (
	"fmt"
	"image"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/render"
)

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
	if g.world.sections[1].renderWidth() != 1500 || g.world.sections[2].renderWidth() != 1000 {
		t.Fatal("resize did not restrict neighbor refreshes to contributing vegetation")
	}
}

func TestResizeRefreshFollowsCameraAndRetainsCutsDuringUpload(t *testing.T) {
	g := resolutionTestScene(t)
	for _, id := range []int64{0, 1} {
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
	for tick := 0; tick < 10 && g.world.upload.stage < uploadVines; tick++ {
		g.Update(viewport)
	}
	if g.world.upload.stage != uploadVines {
		t.Fatal("terrain was not published before vegetation")
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

func TestSectionUploadsPublishTerrainBeforeVegetation(t *testing.T) {
	w := &world{sections: make(map[int64]*worldSection), jobs: make(chan sectionJob, 1), results: make(chan render.SectionMesh, 1), done: make(chan struct{}), working: true}
	// Empty batches isolate the scheduler; nonempty crop bounds exercise image ownership.
	bounds := image.Rect(10, 20, 30, 40)
	w.results <- render.SectionMesh{ID: 0, Background: render.GridMesh{Outlines: make([]render.TriangleMesh, uploadDrawsPerTick*2+1)}, Vines: make([]render.TriangleMesh, uploadDrawsPerTick*2+1), ForegroundVines: make([]render.TriangleMesh, uploadDrawsPerTick*2+1), VinesBounds: bounds, ForegroundVinesBounds: bounds, MushroomsBounds: bounds}
	g, err := NewScene(Config{})
	if err != nil {
		t.Fatal(err)
	}
	g.world.close()
	g.world = w
	defer g.Close()
	w.receive(g)
	if w.upload == nil || w.working {
		t.Fatal("result was not handed off to the upload queue")
	}
	// Neighbor is complete, so readiness below depends only on the pending upload.
	w.sections[1] = &worldSection{}
	published := false
	for tick := 0; tick < 30; tick++ {
		stage, next := w.upload.stage, w.upload.next
		w.receive(g)
		section := w.sections[0]
		if section != nil && !published {
			if section == nil || section.terrain != nil || section.foreground == nil || !section.vegetationPending {
				t.Fatal("foreground was not published before background")
			}
			if w.upload.img != nil || w.upload.foreground != nil {
				t.Fatal("upload retained ownership of published images")
			}
			if w.ensure(-.8, .8, 0) {
				t.Fatal("pending vegetation reported ready")
			}
			published = true
			// Camera jumps must not deallocate the section still being completed.
			w.prune(-100.8, .8, 0)
			if w.sections[0] != section {
				t.Fatal("pruning evicted an active upload")
			}
			w.sections[1] = &worldSection{}
		}
		if w.upload == nil {
			if !published || section == nil || section.vegetationPending {
				t.Fatal("upload did not finish both publication stages")
			}
			if section.vines == nil || section.foregroundVines == nil || section.mushrooms == nil {
				t.Fatal("completed vegetation is missing a layer")
			}
			if section.vines.Bounds() != image.Rect(0, 0, 20, 20) || section.vinesBounds != bounds {
				t.Fatal("vegetation lost its crop or origin")
			}
			if !w.ensure(-.8, .8, 0) {
				t.Fatal("completed viewport is not ready")
			}
			return
		}
		if stage == w.upload.stage && w.upload.next-next > uploadDrawsPerTick {
			t.Fatal("tick submitted too many mesh uploads")
		}
		if (stage == uploadBackgroundOutlines || stage == uploadVines || stage == uploadForegroundVines) && next == 0 && w.upload.stage != stage {
			t.Fatal("large mesh list was uploaded in one tick")
		}
	}
	t.Fatal("section upload did not complete")
}

func TestEarlyTerrainPublicationReusesImagesForVegetation(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.sections[0].deallocate()
	delete(w.sections, 0)
	w.terrainMeshes = make(chan render.SectionMesh, 1)
	w.working = true
	early := resolutionTestMesh(0)
	early.TerrainOnly = true
	w.terrainMeshes <- early
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
	w.results <- complete
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
		results: make(chan render.SectionMesh, 1), terrainMeshes: make(chan render.SectionMesh, 1), canceled: make(chan int64, 1),
		working: true, activeID: 3, upload: &sectionUpload{data: render.SectionMesh{ID: 1}}}
	w.results <- render.SectionMesh{ID: 3}
	w.collect()
	if w.working || w.pending[3].ID != 3 || w.upload.data.ID != 1 {
		t.Fatal("active upload blocked completed CPU work")
	}
	// A late early result must never replace an already collected full result.
	w.terrainMeshes <- render.SectionMesh{ID: 3, TerrainOnly: true}
	w.collect()
	if w.pending[3].TerrainOnly {
		t.Fatal("late terrain replaced a complete section")
	}
	w.working, w.activeID = true, 4
	w.terrainMeshes <- render.SectionMesh{ID: 4, TerrainOnly: true}
	w.canceled <- 4
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
		w.results <- original
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
	plants := mesh
	plants.Layers = render.VegetationLayer
	w.startUpload(plants)
	for tick := 0; tick < 20 && w.upload != nil; tick++ {
		w.receive(g)
	}
	if s.foreground != image || s.vegetationPending || s.mesh.LayerMask() != render.AllLayers {
		t.Fatal("vegetation completion lost an independent layer")
	}
}

func TestRockUploadPreemptsAndResumesVegetation(t *testing.T) {
	for _, layer := range []render.MeshLayers{render.ForegroundLayer, render.BackgroundLayer} {
		t.Run(fmt.Sprint(layer), func(t *testing.T) {
			g := resolutionTestScene(t)
			w := g.world
			plants := resolutionTestMesh(0)
			plants.Layers = render.VegetationLayer
			plants.Vines = make([]render.TriangleMesh, uploadDrawsPerTick*2+1)
			plants.VinesBounds = image.Rect(10, 20, 30, 40)
			w.startUpload(plants)
			w.receive(g) // Start the vegetation phase.
			paused := w.upload
			if paused == nil || paused.stage != uploadVines || paused.next == 0 {
				t.Fatal("vegetation did not begin")
			}
			stage, next := paused.stage, paused.next
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
			if w.upload == nil || w.upload.data.ID != 1 || w.parked[0] != paused {
				t.Fatal("rock layer did not preempt optional upload")
			}
			for tick := 0; tick < 20 && w.upload != nil; tick++ {
				w.receive(g)
			}
			if (layer == render.ForegroundLayer && w.sections[1].foreground == nil) ||
				(layer == render.BackgroundLayer && w.sections[1].terrain == nil) {
				t.Fatal("rock layer did not publish")
			}
			w.receive(g)
			if w.upload != paused || paused.stage != stage || paused.next != next {
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
	if w.upload.stage == uploadForegroundVines && w.upload.next > 0 {
		t.Fatal("vine draws and finishing pass left no draw budget for foreground vines")
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
	if w.upload.foreground != nil {
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
	if u == nil || u.surface == nil || u.faceNext != batch || w.sections[0] != nil {
		t.Fatal("first face batch exceeded its index budget or published incomplete terrain")
	}
	surface := u.surface
	for tick := 0; tick < 10 && w.sections[0] == nil; tick++ {
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
