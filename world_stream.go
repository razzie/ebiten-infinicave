package infinicave

import (
	"context"
	"image"
	"image/color"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Neighboring sections need a refresh only when their retained vegetation
// actually crosses into the viewport. Terrain contributes its owned band.
func meshVisible(mesh render.SectionMesh, viewport Viewport) bool {
	if !render.ValidViewport(viewport) {
		return true
	}
	bottom := viewport.Y + viewport.Height
	top := terrain.SectionTop(mesh.ID)
	if top < bottom && top+SectionHeight > viewport.Y {
		return true
	}
	top = terrain.SectionWindowTop(mesh.ID)
	for _, bounds := range []struct{ min, max int }{
		{mesh.VinesBounds.Min.Y, mesh.VinesBounds.Max.Y},
		{mesh.ForegroundVinesBounds.Min.Y, mesh.ForegroundVinesBounds.Max.Y},
		{mesh.MushroomsBounds.Min.Y, mesh.MushroomsBounds.Max.Y},
	} {
		if bounds.max > bounds.min && top+float64(bounds.min)/render.RasterPixelsPerUnit < bottom &&
			top+float64(bounds.max)/render.RasterPixelsPerUnit > viewport.Y {
			return true
		}
	}
	return false
}

// With lookahead enabled, prepare complete images for the immediate neighbors
// and the extra sections in the direction of travel before they become visible.
func (w *world) preloadSection(id int64, viewport Viewport) bool {
	if w.lookaheadSections == 0 || id < 0 || !render.ValidViewport(viewport) {
		return false
	}
	low, high := visibleSections(viewport.Y, viewport.Height)
	if id >= low-1 && id <= high+1 {
		return true
	}
	return viewport.Velocity < 0 && id > high+1 && id-high-1 <= int64(w.lookaheadSections) ||
		viewport.Velocity > 0 && id < low-1 && low-id-1 <= int64(w.lookaheadSections)
}

func (w *world) meshUploadEligible(mesh render.SectionMesh, viewport Viewport) bool {
	return meshVisible(mesh, viewport) || w.preloadSection(mesh.ID, viewport)
}

func (w *world) savePending(data render.SectionMesh) {
	if w.pending == nil {
		w.pending = make(map[int64]render.SectionMesh)
	}
	if previous, ok := w.pending[data.ID]; ok {
		if previous.LayerMask()&render.ForegroundLayer != 0 && !previous.ForegroundBaseOnly && data.ForegroundBaseOnly {
			// A late/retried base recipe cannot downgrade completed growth.
			data.Layers = data.LayerMask() &^ render.ForegroundLayer
			if data.Layers == 0 {
				return
			}
		}
		data = render.MergeSectionMeshes(previous, data)
	}
	w.pending[data.ID] = data
}

func (u *sectionUpload) deallocate() {
	if u.trim != nil {
		u.trim.Deallocate()
		u.trim = nil
	}
	for i := range u.growth {
		u.growth[i].deallocate()
	}
	for i := range u.plants {
		if u.plants[i].cursor.surface != nil {
			u.plants[i].cursor.surface.Deallocate()
			u.plants[i].cursor.surface = nil
		}
	}
	if u.grassCursor.surface != nil {
		u.grassCursor.surface.Deallocate()
		u.grassCursor.surface = nil
	}
	if u.backgroundCursor.surface != nil {
		u.backgroundCursor.surface.Deallocate()
		u.backgroundCursor.surface = nil
	}
	for _, img := range []*ebiten.Image{u.img, u.foreground, u.vines, u.foregroundVines, u.mushrooms, u.spiderWebs, u.grass, u.surface} {
		if img != nil {
			img.Deallocate()
		}
	}
	u.img, u.foreground, u.vines, u.foregroundVines, u.mushrooms, u.spiderWebs, u.surface = nil, nil, nil, nil, nil, nil, nil
	u.grass = nil
}

// Resize/offscreen abandonment keeps CPU data and all published layers.
func (w *world) deferUpload() {
	u := w.upload
	if u == nil {
		return
	}
	w.savePending(u.data)
	u.deallocate()
	w.upload = nil
}
func (w *world) deferParked() {
	for id, u := range w.parked {
		w.savePending(u.data)
		u.deallocate()
		delete(w.parked, id)
	}
}
func (w *world) startUpload(data render.SectionMesh) {
	if u := w.parked[data.ID]; u != nil {
		w.upload = u
		delete(w.parked, data.ID)
		return
	}
	w.upload = &sectionUpload{data: data, pixels: w.renderWidth()}
	delete(w.pending, data.ID)
}

const (
	uploadRockUnderlay = iota
	uploadForegroundFaces
	uploadForegroundOutlines
	uploadRockGrowth
	uploadGrass
	uploadForegroundPublish
	uploadBackgroundFaces
	uploadBackgroundOutlines
	uploadBackgroundFade
	uploadBackgroundPublish
	uploadVines
	uploadVineFinish
	uploadSpiderWebs
	uploadMushrooms
	uploadForegroundVines
	uploadPlantsPublish
	uploadDone
	uploadGrowthProduct
)

type sectionUpload struct {
	trim                                               *ebiten.Image
	growth                                             [growthProducts]growthUpload
	data, raster                                       render.SectionMesh
	pixels                                             int
	img, foreground, vines, foregroundVines, mushrooms *ebiten.Image
	spiderWebs                                         *ebiten.Image
	grass                                              *ebiten.Image
	uploadCursor
	plants             [vegetationProducts]plantUpload
	plantsInitialized  bool
	grassCursor        uploadCursor
	grassRaster        render.GridMesh
	grassData          *render.SectionMesh
	grassInitialized   bool
	backgroundCursor   uploadCursor
	laneNext           int
	grassFacesDone     bool
	plantNext          int
	lastTurn, progress uint64
	initialized        bool
	needed             render.MeshLayers
}

// A cursor owns one raster product's progress, including its AA accumulator.
// Helpers receive it explicitly so unrelated products can advance independently.
type uploadCursor struct {
	stage, next, faceNext, instancePart int
	surface                             *ebiten.Image
}

// Limit draw submissions per tick as well as separating the large layer
// uploads. CPU tessellation has already finished before a result arrives.
const uploadDrawsPerTick = 16

const uploadTimePerTick = 2 * time.Millisecond

const uploadIndicesPerTick = 128 * 1024

// One budget covers all sections and products advanced in a tick. Draw, index,
// pixel, and submission-time limits bound work without fixed stage barriers.
type uploadBudget struct {
	start          time.Time
	draws, indices int
	pixels         int
	stage          int
	quantumEnd     int
}

func (b *uploadBudget) enter(stage int) bool {
	b.stage = stage
	return b.available()
}

// Pixel work bounds shader passes and AA resolves independently of indices.
// One oversized first submission is permitted so large targets still progress.
const uploadPixelsPerTick = 4 * 1024 * 1024

func (b *uploadBudget) takeRaster(indices, pixels int) bool {
	if b.draws > 0 && b.pixels+pixels > uploadPixelsPerTick {
		return false
	}
	if !b.take(indices) {
		return false
	}
	b.pixels += pixels
	return true
}

func (b *uploadBudget) available() bool {
	return b.draws < uploadDrawsPerTick && (b.draws == 0 ||
		(b.indices < uploadIndicesPerTick && b.pixels < uploadPixelsPerTick && time.Since(b.start) < uploadTimePerTick))
}

func (b *uploadBudget) take(indices int) bool {
	if !b.available() || (b.quantumEnd > 0 && b.draws >= b.quantumEnd) || (b.draws > 0 && b.indices+indices > uploadIndicesPerTick) {
		return false
	}
	b.draws++
	b.indices += indices
	return true
}

func (w *world) uploadMeshes(u *uploadCursor, dst *ebiten.Image, meshes []render.TriangleMesh, budget *uploadBudget) bool {
	if u.next < len(meshes) && !budget.enter(u.stage) {
		return false
	}
	for ; u.next < len(meshes); u.next++ {
		if !budget.take(len(meshes[u.next].Indices)) {
			break
		}
		meshes[u.next].DrawWithAntialiasing(dst, w.white, w.antialiasing.GeometryAA())
	}
	return u.next == len(meshes)
}

// Collect CPU results even while another section uploads. Requests, pending
// results, and paused uploads share a bounded queue.
func (w *world) collect() {
	// Drain early products first. Every envelope is checked against its flight;
	// old finals and cancellation acknowledgements cannot finish a newer job.
	for {
		select {
		case result := <-w.terrainMeshes:
			if !w.matchesFlight(result.ID, result.request) {
				continue
			}
			if flight, active := w.inflight[result.ID]; !active || flight.ctx.Err() == nil {
				w.savePending(result.SectionMesh)
			}
		default:
			goto results
		}
	}
results:
	for {
		select {
		case result := <-w.results:
			if !w.matchesFlight(result.ID, result.request) {
				continue
			}
			flight, active := w.inflight[result.ID]
			completed := !active || flight.ctx.Err() == nil
			if completed {
				w.savePending(result.SectionMesh)
			} else {
				w.discardUnpublished(result.ID)
			}
			w.finishJob(result.ID, completed)
		default:
			goto canceled
		}
	}
canceled:
	for {
		select {
		case cancellation := <-w.canceled:
			if w.matchesFlight(cancellation.id, cancellation.request) {
				w.finishJob(cancellation.id, false)
				w.discardUnpublished(cancellation.id)
			}
		default:
			return
		}
	}
}

func (w *world) finishJob(id int64, completed bool) {
	if !completed {
		delete(w.requests, id)
	}
	if w.inflight != nil {
		if flight, active := w.inflight[id]; active {
			if completed {
				flight.cancel(errSectionComplete)
			} else {
				flight.cancel(context.Canceled)
			}
			delete(w.inflight, id)
		}
		w.working = len(w.inflight) != 0
		return
	}
	w.working = false
	if w.activeCancel != nil {
		w.activeCancel()
		w.activeCancel = nil
	}
}

// A stream can contain every plant but still lack terrain or growth.
func incompleteRecipe(mesh render.SectionMesh) bool {
	return mesh.LayerMask() != render.AllLayers || mesh.ForegroundBaseOnly
}

// Cancellation releases private work, while published siblings retain their
// recipes, images, reveal clocks, and collision coverage until normal eviction.
func (w *world) discardUnpublished(id int64) {
	if u := w.parked[id]; u != nil {
		u.deallocate()
		delete(w.parked, id)
	}
	if mesh, ok := w.pending[id]; ok && incompleteRecipe(mesh) {
		delete(w.pending, id)
	}
	if w.upload != nil && w.upload.data.ID == id && incompleteRecipe(w.upload.data) {
		w.deferUpload()
		delete(w.pending, id)
	}
	if section := w.sections[id]; section != nil && section.geometry != nil {
		return
	}
	if _, ok := w.collision[id]; ok {
		delete(w.collision, id)
		w.collisionRevision++
	}
}

func (w *world) needsForeground(mesh render.SectionMesh) bool {
	if mesh.LayerMask()&w.viewportLayers(mesh.ID)&render.ForegroundLayer == 0 {
		return false
	}
	section := w.sections[mesh.ID]
	return section == nil || section.foreground == nil || section.renderWidth() != w.renderWidth() || !terrain.SameTerrain(section.geometry, mesh.Geometry)
}

// Lookahead prepares all layers before exposure. Without lookahead, neighboring
// windows upload only crossing plants and retain their rock recipes for scrolling.
func (w *world) viewportLayers(id int64) render.MeshLayers {
	if render.ValidViewport(w.viewport) && !w.preloadSection(id, w.viewport) {
		low, high := visibleSections(w.viewport.Y, w.viewport.Height)
		if id < low || id > high {
			return render.VegetationLayer
		}
	}
	layers := render.AllLayers
	if w.disableBackground {
		layers &^= render.BackgroundLayer
	}
	return layers
}

func (w *world) needsVisibleRefresh(id int64, s *worldSection) bool {
	if w.viewportLayers(id)&render.ForegroundLayer != 0 {
		return s.needsRefresh(w.renderWidth(), w.viewportLayers(id))
	}
	return s.needsRefresh(w.renderWidth(), render.VegetationLayer)
}

func (w *world) uploadPriority(mesh render.SectionMesh) int {
	priority := 2 // Vegetation follows both rock layers.
	if w.needsForeground(mesh) {
		priority = 0
	} else if mesh.LayerMask()&w.viewportLayers(mesh.ID)&render.BackgroundLayer != 0 {
		s := w.sections[mesh.ID]
		if s == nil || s.terrain == nil || s.backgroundWidth() != w.renderWidth() || s.mesh == nil || !sameGridMesh(s.mesh.Background, mesh.Background) {
			priority = 1
		}
	}
	if render.ValidViewport(w.viewport) {
		low, high := visibleSections(w.viewport.Y, w.viewport.Height)
		if mesh.ID < low || mesh.ID > high {
			priority += 3
			if !meshVisible(mesh, w.viewport) {
				// Finish the nearest offscreen section's decorations before
				// starting farther rock layers. Visible layers come first.
				priority += 3 * int(max(low-mesh.ID, mesh.ID-high))
			}
		}
	}
	return priority
}

// Advance runnable sections within one shared budget. Equal-priority sections
// rotate after each quantum, retaining private raster cursors and surfaces.
func (w *world) receive(g *Scene) {
	budget := uploadBudget{start: time.Now()}
	w.receiveBudget(g, &budget)
}

func (w *world) receiveBudget(g *Scene, budget *uploadBudget) {
	g.flushCarveImages()
	w.collect()
	w.absorbPending(g)
	blocked := make(map[*sectionUpload]bool)
	idle := make(map[*sectionUpload]int)
	refreshed := make(map[int64]bool)
	for w.selectUpload(blocked, refreshed) {
		u := w.upload
		// A metadata-only completion can leave aggregate readiness pending.
		// Do not recreate its cached envelope repeatedly without spending budget.
		refreshed[u.data.ID] = true
		before, progress := budget.draws, u.progress
		w.uploadTurn++
		u.lastTurn = w.uploadTurn
		budget.quantumEnd = budget.draws + 4
		w.advanceUpload(g, u, budget)
		if w.upload == u && budget.draws == before && u.progress == progress {
			// Try every independent lane before declaring this section
			// blocked by dependencies or the remaining frame budget.
			idle[u]++
			lanes := 0
			for _, mask := range []render.MeshLayers{render.ForegroundLayer, render.BackgroundLayer, render.VegetationLayer, grassUploadLayer, underGrowthUploadLayer, surfaceGrowthUploadLayer} {
				if u.needed&mask != 0 {
					lanes++
				}
			}
			if idle[u] >= lanes {
				blocked[u] = true
			}
		} else {
			idle[u] = 0
		}
		if !budget.available() {
			return
		}
	}
}

func (w *world) selectUpload(blocked map[*sectionUpload]bool, refreshed map[int64]bool) bool {
	var selected render.SectionMesh
	var selectedUpload *sectionUpload
	found := false
	choose := func(mesh render.SectionMesh, u *sectionUpload) {
		if blocked[u] || !w.meshUploadEligible(mesh, w.viewport) {
			return
		}
		turn, selectedTurn := uint64(0), uint64(0)
		if u != nil {
			turn = u.lastTurn
		}
		if selectedUpload != nil {
			selectedTurn = selectedUpload.lastTurn
		}
		priority := w.uploadPriority(mesh)
		if !found || priority < w.uploadPriority(selected) ||
			(priority == w.uploadPriority(selected) && (turn < selectedTurn || turn == selectedTurn && mesh.ID < selected.ID)) {
			selected, selectedUpload, found = mesh, u, true
		}
	}
	if w.upload != nil {
		choose(w.upload.data, w.upload)
	}
	for _, u := range w.parked {
		choose(u.data, u)
	}
	for id, mesh := range w.pending {
		if w.parked[id] == nil && (w.upload == nil || w.upload.data.ID != id) {
			choose(mesh, nil)
		}
	}
	if render.ValidViewport(w.viewport) {
		for id, section := range w.sections {
			if refreshed[id] || section.mesh == nil || !w.needsVisibleRefresh(id, section) {
				continue
			}
			if _, pending := w.pending[id]; pending || w.parked[id] != nil || w.upload != nil && w.upload.data.ID == id {
				continue
			}
			choose(*section.mesh, nil)
		}
	}
	if !found {
		return false
	}
	if w.upload != nil && selected.ID != w.upload.data.ID {
		if w.parked == nil {
			w.parked = make(map[int64]*sectionUpload)
		}
		w.parked[w.upload.data.ID] = w.upload
		w.upload = nil
	}
	if w.upload == nil {
		w.startUpload(selected)
	}
	return true
}

func (w *world) advanceUpload(g *Scene, u *sectionUpload, budget *uploadBudget) {
	if u.pixels == 0 {
		u.pixels = w.renderWidth()
	}
	if !u.initialized {
		g.applyStoredCuts(&u.data)
		u.needed = u.data.LayerMask() & w.viewportLayers(u.data.ID)
		section := w.sections[u.data.ID]
		if section != nil {
			if section.foreground != nil && section.renderWidth() == u.pixels && terrain.SameTerrain(section.geometry, u.data.Geometry) {
				u.needed &^= render.ForegroundLayer
			}
			if section.terrain != nil && section.backgroundWidth() == u.pixels && section.mesh != nil && sameGridMesh(section.mesh.Background, u.data.Background) {
				u.needed &^= render.BackgroundLayer
			}
			for kind := vegetationProduct(0); kind < vegetationProducts; kind++ {
				if !section.needsPlant(u.data, kind, u.pixels) {
					u.needed &^= kind.layer()
				}
			}

		}
		if u.data.LayerMask()&render.ForegroundLayer != 0 && !u.data.ForegroundBaseOnly &&
			(section == nil || section.needsGrass(u.data, u.pixels)) {
			u.needed |= grassUploadLayer
		}
		if !u.data.ForegroundBaseOnly && u.data.LayerMask()&render.ForegroundLayer != 0 {
			for kind := growthProduct(0); kind < growthProducts; kind++ {
				if section == nil || section.needsGrowth(u.data, kind, u.pixels) {
					u.needed |= kind.layer()
				}
			}
		}
		u.initialized = true
		u.stage = u.firstStage()
	}
	if u.stage == uploadDone {
		previous := w.sections[u.data.ID]
		newGeometry := u.data.Geometry != nil && (previous == nil || previous.geometry == nil)
		w.updateSectionSource(u)
		if newGeometry {
			w.revision++
		}
		w.upload = nil
		return
	}
	if w.white == nil {
		w.white = ebiten.NewImage(1, 1)
		w.white.Fill(color.White)
	}
	// Each terrain layer has its own cursor and supersampled surface.
	// Rotate with vegetation even when a sibling has not published yet.
	for range 6 {
		lane := u.laneNext
		u.laneNext = (u.laneNext + 1) % 6
		if lane == 0 && u.needed&render.ForegroundLayer != 0 {
			break
		}
		if lane == 1 && u.needed&render.BackgroundLayer != 0 {
			w.advanceBackground(g, u, budget)
			return
		}
		if lane == 2 && u.needed&render.VegetationLayer != 0 {
			w.advancePlants(g, u, budget)
			return
		}
		if lane == 3 && u.needed&grassUploadLayer != 0 {
			w.advanceGrass(g, u, budget)
			return
		}
		if lane >= 4 {
			kind := growthProduct(lane - 4)
			if u.needed&kind.layer() != 0 {
				w.advanceGrowth(g, u, kind, budget)
				return
			}
		}
	}
	top := terrain.SectionWindowTop(u.data.ID)
	for {
		switch u.stage {
		case uploadRockUnderlay:
			if !budget.available() {
				return
			}
			if u.foreground == nil {
				u.raster.Foreground = render.ScaleGridMesh(render.MaskForeground(u.data.Foreground, render.StructuralSpan), u.pixels, 0)
				u.foreground = render.NewSectionImageAt(terrain.SectionHeight, u.pixels)
			}
			if !w.uploadAttachments(g, &u.uploadCursor, u.foreground, u.raster.Foreground.Growth, false, true, budget) {
				return
			}
		case uploadForegroundFaces:
			if !budget.available() {
				return
			}
			if u.foreground == nil {
				u.raster.Foreground = render.ScaleGridMesh(render.MaskForeground(u.data.Foreground, render.StructuralSpan), u.pixels, 0)
				u.foreground = render.NewSectionImageAt(terrain.SectionHeight, u.pixels)
			}
			if !w.uploadFaces(g, &u.uploadCursor, u.foreground, u.raster.Foreground.Faces, top, true, budget) {
				return
			}
		case uploadForegroundOutlines:
			if !w.uploadMeshes(&u.uploadCursor, u.foreground, u.raster.Foreground.Outlines, budget) {
				return
			}
		case uploadRockGrowth:
			if u.trim == nil && render.HasForegroundSpan(u.raster.Foreground, render.StructuralTopSpan) {
				if !budget.available() {
					return
				}
				u.trim = render.NewSectionImageAt(terrain.SectionHeight, u.pixels)
			}
			if !w.uploadAttachments(g, &u.uploadCursor, u.trim, u.raster.Foreground.Growth, false, false, budget) {
				return
			}
		case uploadForegroundPublish:
			u.progress++
			g.maskUploadForeground(u)
			section := w.sectionForUpload(u)
			changed := section.foreground == nil || !terrain.SameTerrain(section.geometry, u.data.Geometry)
			w.beginForegroundReveal(section, u)
			if section.foreground != nil {
				section.foreground.Deallocate()
			}
			if section.trim != nil {
				section.trim.Deallocate()
			}
			section.trim, u.trim = u.trim, nil
			section.foregroundSplit = true
			section.foreground, u.foreground = u.foreground, nil
			if section.carveBase != nil {
				section.carveBase.Deallocate()
				section.carveBase = nil
				section.carveBaseCuts = 0
			}
			source := render.StructuralSection(u.data)
			section.rockSource = &source
			// Keep immutable baked rock pixels while publishing, before any
			// impact. Dirty cut regions can restore them with one GPU copy.
			if section.foreground != nil {
				section.carveBase = ebiten.NewImage(section.foreground.Bounds().Dx(), section.foreground.Bounds().Dy())
				section.carveBase.DrawImage(section.foreground, nil)
				section.carveBaseCuts = len(source.VisualCuts)
			}
			section.pixels = u.pixels
			section.geometry = u.data.Geometry
			w.updateSectionSource(u)
			delete(w.collision, u.data.ID)
			if changed {
				w.revision++
			}
			u.needed &^= render.ForegroundLayer
			u.stage = u.firstStage()
			u.next = 0
			return

		}
		u.stage++
		if u.stage == uploadGrass {
			u.stage++
		}
		u.next = 0
	}
}

func sameGridMesh(a, b render.GridMesh) bool {
	return len(a.Faces.Vertices) == len(b.Faces.Vertices) && (len(a.Faces.Vertices) == 0 || &a.Faces.Vertices[0] == &b.Faces.Vertices[0])
}
func (u *sectionUpload) firstStage() int {
	if u.needed&render.ForegroundLayer != 0 {
		return uploadRockUnderlay
	}
	if u.needed&render.BackgroundLayer != 0 {
		return uploadBackgroundFaces
	}
	if u.needed&grassUploadLayer != 0 {
		return uploadGrass
	}
	if u.needed&render.VegetationLayer != 0 {
		return uploadVines
	}
	if u.needed&(underGrowthUploadLayer|surfaceGrowthUploadLayer) != 0 {
		return uploadGrowthProduct
	}
	return uploadDone
}
func (w *world) sectionForUpload(u *sectionUpload) *worldSection {
	s := w.sections[u.data.ID]
	if s == nil {
		s = &worldSection{vegetationPending: true, grassPending: true}
		w.sections[u.data.ID] = s
	}
	if s.backgroundPixels == 0 {
		s.backgroundPixels = s.renderWidth()
	}
	if s.vegetationPixels == 0 {
		s.vegetationPixels = s.renderWidth()
	}
	return s
}
func (w *world) updateSectionSource(u *sectionUpload) {
	s := w.sectionForUpload(u)
	source := u.data
	if s.mesh != nil {
		source = render.MergeSectionMeshes(*s.mesh, source)
	}
	s.mesh = &source
	if source.Geometry != nil {
		s.geometry = source.Geometry
	}
}

// Draw chunks into one persistent 2x surface, then resolve exactly once. Splitting
// native antialiased draws would blend triangle edges repeatedly at batch seams.
func (w *world) uploadFaces(g *Scene, u *uploadCursor, dst *ebiten.Image, mesh render.TriangleMesh, top float64, material bool, budget *uploadBudget) bool {
	if len(mesh.Indices) == 0 {
		return true
	}
	if !budget.enter(u.stage) {
		return false
	}
	if !w.antialiasing.GeometryAA() {
		remaining := uploadIndicesPerTick - budget.indices
		end := min(u.faceNext+(remaining/3)*3, len(mesh.Indices))
		if end <= u.faceNext || !budget.takeRaster(end-u.faceNext, dst.Bounds().Dx()*dst.Bounds().Dy()) {
			return false
		}
		chunk := mesh
		chunk.Indices = mesh.Indices[u.faceNext:end]
		if material {
			g.drawGridFacesWithAA(dst, chunk, top, false)
		} else {
			chunk.DrawUnaliased(dst, w.white)
		}
		u.faceNext = end
		if end < len(mesh.Indices) {
			return false
		}
		u.faceNext = 0
		return true
	}
	if u.surface == nil {
		size := dst.Bounds().Size()
		u.surface = ebiten.NewImageWithOptions(image.Rect(0, 0, size.X*2, size.Y*2), &ebiten.NewImageOptions{Unmanaged: true})
		u.faceNext = 0
	}
	if u.faceNext < len(mesh.Indices) {
		remaining := uploadIndicesPerTick - budget.indices
		end := min(u.faceNext+(remaining/3)*3, len(mesh.Indices))
		if end <= u.faceNext || !budget.takeRaster(end-u.faceNext, 4*dst.Bounds().Dx()*dst.Bounds().Dy()) {
			return false
		}
		chunk := render.SupersampledBatch(mesh, u.faceNext, end)
		if material {
			g.drawGridFacesWithAA(u.surface, chunk, top, false)
		} else {
			chunk.DrawUnaliased(u.surface, w.white)
		}
		u.faceNext = end
		if end < len(mesh.Indices) {
			return false
		}
	}
	if !budget.takeRaster(0, dst.Bounds().Dx()*dst.Bounds().Dy()) {
		return false
	}
	op := &ebiten.DrawImageOptions{Filter: ebiten.FilterLinear}
	op.GeoM.Scale(.5, .5)
	dst.DrawImage(u.surface, op)
	u.surface.Deallocate()
	u.surface = nil
	u.faceNext = 0
	return true
}

func (u *sectionUpload) clearSurface() {
	if u.surface != nil {
		u.surface.Deallocate()
		u.surface = nil
	}
	u.faceNext = 0
}

// New products join private work for their section immediately. Overlapping
// replacements retain their pending envelope until the current recipe finishes.
func (w *world) absorbPending(g *Scene) {
	absorb := func(u *sectionUpload) {
		if u == nil {
			return
		}
		next, ok := w.pending[u.data.ID]
		if !ok {
			return
		}
		// A full foreground recipe may arrive while the base is private.
		// Admit its details without replacing the foreground cursor's recipe.
		if u.initialized && u.data.ForegroundBaseOnly && next.LayerMask()&render.ForegroundLayer != 0 && !next.ForegroundBaseOnly && u.grassData == nil {
			grass := next
			g.applyStoredCuts(&grass)
			u.grassData = &grass
			if w.viewportLayers(next.ID)&render.ForegroundLayer != 0 {
				u.needed |= grassUploadLayer | underGrowthUploadLayer | surfaceGrowthUploadLayer
				if u.stage == uploadDone || u.stage == uploadVines {
					u.stage = u.firstStage()
				}
			}
		}
		fresh := next.LayerMask() &^ u.data.LayerMask()
		if fresh == 0 {
			return
		}
		remaining := next.LayerMask() &^ fresh
		part := next
		part.Layers = fresh
		g.applyStoredCuts(&part)
		u.data = render.MergeSectionMeshes(u.data, part)
		if u.initialized {
			added := fresh & w.viewportLayers(u.data.ID)
			u.needed |= added
			if added&render.ForegroundLayer != 0 && !u.data.ForegroundBaseOnly {
				u.needed |= grassUploadLayer | underGrowthUploadLayer | surfaceGrowthUploadLayer
			}
			if u.plantsInitialized && added&render.VegetationLayer != 0 {
				w.addPlantUploads(u, added&render.VegetationLayer)
			}
			if u.stage == uploadDone || u.stage == uploadVines || added&render.ForegroundLayer != 0 {
				u.stage = u.firstStage()
			}
		}
		if remaining == 0 {
			delete(w.pending, u.data.ID)
		} else {
			next.Layers = remaining
			w.pending[u.data.ID] = next
		}
	}
	absorb(w.upload)
	for _, u := range w.parked {
		absorb(u)
	}
}
