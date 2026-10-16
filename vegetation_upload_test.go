package infinicave

import (
	"bytes"
	"image"
	"testing"
	"time"

	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Runs inside the carving test's game loop. A published plant must accept edits
// even while both its supporting rock image and sibling vines are unfinished.
func checkPartialVegetationCarvePixels(t *testing.T) {
	t.Helper()
	g, err := NewScene(Config{DisableBackground: true})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	g.world.close()
	data := carvePixelFixture()
	geometry := terrain.PrepareTerrainGeometry(data, 0)
	w := &world{sections: make(map[int64]*worldSection), done: make(chan struct{}), pixels: 256,
		disableBackground: true, collision: map[int64]*terrain.Geometry{0: geometry}}
	g.world = w
	mesh := render.PrepareSection(data, ViewShaded, render.RockGrowthOptions{})
	mesh.Geometry = geometry
	render.FinishSectionMesh(&mesh)
	mesh.Foreground.Outlines = append(make([]render.TriangleMesh, uploadDrawsPerTick*128), mesh.Foreground.Outlines...)
	w.startUpload(mesh)
	for tick := 0; tick < 100 && (w.sections[0] == nil || w.sections[0].mushrooms == nil); tick++ {
		receiveTestBudget(g)
	}
	s := w.sections[0]
	if s == nil || s.mushrooms == nil || s.foreground != nil || w.upload == nil {
		t.Fatal("fixture did not publish mushrooms before the unfinished rock image")
	}
	read := func() []byte {
		if s.mushrooms == nil {
			return nil
		}
		pixels := make([]byte, 4*s.mushrooms.Bounds().Dx()*s.mushrooms.Bounds().Dy())
		s.mushrooms.ReadPixels(pixels)
		return pixels
	}
	before := read()
	root := s.mesh.Plants[0].Anchors[0].Add(V{Y: terrain.SectionTop(0)})
	if _, err := g.CarveCircle(root, .006); err != nil {
		t.Fatal(err)
	}
	g.flushCarveImages()
	if !s.mesh.Plants[0].Dead || bytes.Equal(before, read()) {
		t.Fatal("carving left the already-published mushroom unchanged")
	}
	finish := func() {
		for tick := 0; tick < 500 && w.upload != nil; tick++ {
			receiveTestBudget(g)
		}
		if w.upload != nil {
			t.Fatal("partial-publication fixture stalled")
		}
	}
	finish()
	want := read()
	s.deallocate()
	delete(w.sections, 0)
	w.startUpload(mesh)
	finish()
	s = w.sections[0]
	if !bytes.Equal(want, read()) {
		t.Fatal("cutting an independently published product differs from replay on reload")
	}
	// Replay the same cut when the vegetation recipe arrives during an existing
	// terrain upload, rather than in its initial envelope.
	s.deallocate()
	delete(w.sections, 0)
	initial := mesh
	initial.Layers = render.ForegroundLayer | render.BackgroundVinesLayer
	initial.TerrainOnly = true
	w.startUpload(initial)
	receiveTestBudget(g)
	late := mesh
	late.Layers = render.MushroomsLayer | render.WebsLayer | render.ForegroundVinesLayer
	w.savePending(late)
	finish()
	s = w.sections[0]
	if !bytes.Equal(want, read()) {
		t.Fatal("late CPU recipe restored carved vegetation")
	}

}

// Ignore wall-clock submission time in scheduler tests. Draw/index/pixel limits
// still apply, so assertions don't depend on shader warmup or machine speed.
func receiveTestBudget(g *Scene) uploadBudget {
	b := uploadBudget{start: time.Now().Add(time.Hour)}
	g.world.receiveBudget(g, &b)
	return b
}

func TestVegetationPublishesWithoutWaitingForVines(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.pixels = 64
	w.appearanceFade = &render.FadeRenderer{} // Publication clocks need no shader.
	data := carvePixelFixture()
	mesh := render.PrepareVegetation(data, ViewShaded)
	mesh.Geometry = terrain.PrepareTerrainGeometry(data, 0)
	mesh.Vines = append(make([]render.TriangleMesh, uploadDrawsPerTick*64), mesh.Vines...)
	w.sections[0].vegetationPending = true
	w.savePending(mesh)
	for tick := 0; tick < 100 && w.sections[0].mushrooms == nil; tick++ {
		receiveTestBudget(g)
	}
	s := w.sections[0]
	if s.mushrooms == nil || s.vines != nil || !s.vegetationPending {
		t.Fatal("mushrooms waited for the unfinished background vine product")
	}
	if s.plantWidth(mushroomsProduct) != 64 || s.plantReveal(mushroomsProduct).Progress() != 0 {
		t.Fatal("publication did not set the product's width and reveal clock")
	}
	mushrooms := s.mushrooms
	for range 10 {
		s.plantReveal(mushroomsProduct).Update()
	}
	progress := s.plantReveal(mushroomsProduct).Progress()
	for tick := 0; tick < 200 && w.upload != nil; tick++ {
		receiveTestBudget(g)
	}
	if w.upload != nil || s.vegetationPending || s.vines == nil {
		t.Fatal("remaining vegetation did not complete")
	}
	if s.mushrooms != mushrooms || s.plantReveal(mushroomsProduct).Progress() != progress || s.plantReveal(backgroundVinesProduct).Progress() != 0 {
		t.Fatal("a sibling publication replaced the mushroom image or restarted its reveal")
	}
}

func TestReadySectionsShareUploadQuanta(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.pixels = 64
	for id := int64(0); id < 2; id++ {
		mesh := resolutionTestMesh(id)
		mesh.Layers = render.VegetationLayer
		mesh.Vines = make([]render.TriangleMesh, uploadDrawsPerTick*8)
		mesh.VinesBounds = image.Rect(10, 1000, 330, 1320)
		w.sections[id].vegetationPending = true
		w.savePending(mesh)
	}
	b := receiveTestBudget(g)
	progress := 0
	for id := int64(0); id < 2; id++ {
		u := w.parked[id]
		if w.upload != nil && w.upload.data.ID == id {
			u = w.upload
		}
		if u == nil || u.plants[backgroundVinesProduct].cursor.next == 0 {
			t.Fatal("an equally urgent section monopolized the upload budget")
		}
		progress += u.plants[backgroundVinesProduct].cursor.next
	}
	if progress > uploadDrawsPerTick || b.draws > uploadDrawsPerTick {
		t.Fatal("sections did not share the same draw budget")
	}
}

func TestSmallQueuedProductsPublishInOneUpdate(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.pixels = 16
	for _, id := range []int64{10, 11} {
		mesh := resolutionTestMesh(id)
		mesh.Layers, mesh.TerrainOnly = render.ForegroundLayer, true
		w.savePending(mesh)
	}
	receiveTestBudget(g)
	for _, id := range []int64{10, 11} {
		if s := w.sections[id]; s == nil || s.foreground == nil {
			t.Fatal("starting or publishing another product introduced a mandatory tick")
		}
	}
	if w.upload != nil || len(w.parked) != 0 || len(w.pending) != 0 {
		t.Fatal("completed products retained staging ownership")
	}
}

func TestSmallProductUsesBudgetThatCannotFitTerrain(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.sections[0].deallocate()
	delete(w.sections, 0)
	mesh := resolutionTestMesh(0)
	mesh.Foreground.Growth = nil
	mesh.MushroomsBounds = image.Rect(10, 1000, 30, 1020)
	w.savePending(mesh)
	// The 2x rock pass cannot fit; the cropped mushroom product can.
	b := uploadBudget{start: time.Now().Add(time.Hour), draws: 1, pixels: 3 * 1024 * 1024}
	w.receiveBudget(g, &b)
	s := w.sections[0]
	if s == nil || s.mushrooms == nil || s.foreground != nil {
		t.Fatal("a terrain pass blocked an independently runnable small product")
	}
}

func TestVegetationResizeKeepsEachPublishedWidth(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.pixels = 64
	mesh := render.PrepareVegetation(carvePixelFixture(), ViewShaded)
	mesh.Geometry = w.sections[0].geometry
	w.sections[0].vegetationPending = true
	w.startUpload(mesh)
	for tick := 0; tick < 100 && w.upload != nil; tick++ {
		receiveTestBudget(g)
	}
	s := w.sections[0]
	if s.mushrooms == nil || s.vines == nil {
		t.Fatal("fixture did not publish vegetation")
	}
	oldVines := s.vines
	g.SetRenderWidth(96)
	mesh.Vines = append(make([]render.TriangleMesh, uploadDrawsPerTick*64), mesh.Vines...)
	w.startUpload(mesh)
	for tick := 0; tick < 100 && s.plantWidth(mushroomsProduct) != 96; tick++ {
		receiveTestBudget(g)
	}
	if s.plantWidth(mushroomsProduct) != 96 || s.plantWidth(backgroundVinesProduct) != 64 || s.vines != oldVines {
		t.Fatal("partial resize changed an unfinished product's displayed width or fallback")
	}
	g.SetRenderWidth(128)
	if w.upload != nil || len(w.parked) != 0 || s.plantWidth(mushroomsProduct) != 96 || s.vines != oldVines {
		t.Fatal("resize discarded published products instead of private staging")
	}
}

func TestLateVegetationJoinsActiveUpload(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.pixels = 64
	data := carvePixelFixture()
	vines := render.PrepareVegetationParts(data, ViewShaded, terrain.BackgroundVinesPart)
	vines.Geometry = w.sections[0].geometry
	vines.Vines = append(make([]render.TriangleMesh, uploadDrawsPerTick*64), vines.Vines...)
	w.sections[0].vegetationPending = true
	w.savePending(vines)
	receiveTestBudget(g)
	u := w.upload
	if u == nil || u.plants[backgroundVinesProduct].cursor.next == 0 {
		t.Fatal("fixture did not start vines")
	}
	next := u.plants[backgroundVinesProduct].cursor.next
	mushrooms := render.PrepareVegetationParts(data, ViewShaded, terrain.MushroomsPart)
	mushrooms.Geometry = vines.Geometry
	w.savePending(mushrooms)
	for tick := 0; tick < 20 && w.sections[0].mushrooms == nil; tick++ {
		receiveTestBudget(g)
	}
	s := w.sections[0]
	if s.mushrooms == nil || w.upload != u || s.vines != nil {
		t.Fatal("late mushrooms waited for the active vine upload")
	}
	if u.plants[backgroundVinesProduct].cursor.next < next {
		t.Fatal("late recipe restarted vine progress")
	}
	if !s.vegetationPending {
		t.Fatal("unknown products counted as complete")
	}
	image := s.mushrooms
	// A final cumulative envelope repeats recipes without replacing their images.
	rest := render.PrepareVegetationParts(data, ViewShaded, terrain.WebsPart|terrain.ForegroundVinesPart)
	rest.Geometry = vines.Geometry
	final := render.MergeSectionMeshes(render.MergeSectionMeshes(vines, mushrooms), rest)
	w.savePending(final)
	for tick := 0; tick < 300 && (w.upload != nil || len(w.pending) != 0); tick++ {
		receiveTestBudget(g)
	}
	if w.upload != nil || s.vegetationPending || s.mushrooms != image {
		t.Fatal("final envelope stalled or republished an unchanged product")
	}
}

func TestEmptyVegetationProductsRemainIndependent(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	w.pixels = 64
	s := w.sections[0]
	s.vegetationPending = true
	for kind := vegetationProduct(0); kind < vegetationProducts; kind++ {
		mesh := render.SectionMesh{ID: 0, Layers: kind.layer(), TerrainOnly: true, Geometry: s.geometry}
		mesh.PlantRecipes[kind] = new(uint8)
		w.savePending(mesh)
		receiveTestBudget(g)
		if s.plants[kind].pixels != 64 {
			t.Fatalf("empty product %v was not published", kind)
		}
		if s.vegetationPending != (kind != foregroundVinesProduct) {
			t.Fatal("empty product completed its unknown siblings")
		}
		if s.needsRefresh(64, kind.layer()) {
			t.Fatal("published empty product schedules repeated refreshes")
		}
	}
}

func TestCancelAfterAllPlantsBeforeGrowthPreservesPublishedProducts(t *testing.T) {
	g := resolutionTestScene(t)
	w := g.world
	// All plants can finish while the foreground growth recipe is still running.
	source := resolutionTestMesh(0)
	source.ForegroundBaseOnly = true
	s := w.sections[0]
	s.mesh = &source
	s.vegetationPending = false
	if w.ensure(-.8, .8, 0) {
		t.Fatal("full readiness ignored unfinished foreground growth")
	}
	// Pending vegetation on its own is also incomplete, even with all four bits.
	plants := render.SectionMesh{ID: 0, Layers: render.VegetationLayer}
	w.pending = map[int64]render.SectionMesh{0: plants}
	w.discardUnpublished(0)
	if w.sections[0] != s || len(w.pending) != 0 {
		t.Fatal("cancellation removed published products or retained private recipes")
	}
}
