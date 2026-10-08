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

func (w *world) savePending(data render.SectionMesh) {
	if w.pending == nil {
		w.pending = make(map[int64]render.SectionMesh)
	}
	if previous, ok := w.pending[data.ID]; ok {
		if !previous.TerrainOnly && data.TerrainOnly {
			return
		}
		data = render.MergeSectionMeshes(previous, data)
	}
	w.pending[data.ID] = data
}

func (u *sectionUpload) deallocate() {
	for _, img := range []*ebiten.Image{u.img, u.foreground, u.vines, u.foregroundVines, u.mushrooms, u.surface} {
		if img != nil {
			img.Deallocate()
		}
	}
	u.img, u.foreground, u.vines, u.foregroundVines, u.mushrooms, u.surface = nil, nil, nil, nil, nil, nil
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
	uploadForegroundFaces = iota
	uploadForegroundOutlines
	uploadForegroundPublish
	uploadBackgroundFaces
	uploadBackgroundOutlines
	uploadBackgroundFade
	uploadBackgroundPublish
	uploadVines
	uploadVineFinish
	uploadMushrooms
	uploadForegroundVines
	uploadPlantsPublish
	uploadDone
)

type sectionUpload struct {
	data, raster                                       render.SectionMesh
	pixels                                             int
	img, foreground, vines, foregroundVines, mushrooms *ebiten.Image
	stage, next                                        int
	initialized                                        bool
	needed                                             render.MeshLayers
	surface                                            *ebiten.Image
	faceNext                                           int
}

// Limit draw submissions per tick as well as separating the large layer
// uploads. CPU tessellation has already finished before a result arrives.
const uploadDrawsPerTick = 16

const uploadTimePerTick = 2 * time.Millisecond

const uploadIndicesPerTick = 128 * 1024

// One budget covers every stage advanced in a tick. GPU stages stay on
// separate ticks because CPU submission time cannot predict GPU cost. Empty
// stages and publication can advance immediately. A single oversized mesh may
// exceed the index budget so it can still make progress.
type uploadBudget struct {
	start          time.Time
	draws, indices int
	stage          int
}

func (b *uploadBudget) enter(stage int) bool {
	if b.draws > 0 && b.stage != stage {
		return false
	}
	b.stage = stage
	return b.available()
}

func (b *uploadBudget) available() bool {
	return b.draws < uploadDrawsPerTick && (b.draws == 0 ||
		(b.indices < uploadIndicesPerTick && time.Since(b.start) < uploadTimePerTick))
}

func (b *uploadBudget) take(indices int) bool {
	if !b.available() || (b.draws > 0 && b.indices+indices > uploadIndicesPerTick) {
		return false
	}
	b.draws++
	b.indices += indices
	return true
}

func (w *world) uploadMeshes(dst *ebiten.Image, meshes []render.TriangleMesh, budget *uploadBudget) bool {
	u := w.upload
	if u.next < len(meshes) && !budget.enter(u.stage) {
		return false
	}
	for ; u.next < len(meshes); u.next++ {
		if !budget.take(len(meshes[u.next].Indices)) {
			break
		}
		meshes[u.next].Draw(dst, w.white)
	}
	return u.next == len(meshes)
}

// Collect CPU results even while another section uploads. Requests, pending
// results, and paused uploads share a bounded queue.
func (w *world) collect() {
	// Each job sends its early mesh before its final result. Drain the early
	// channel first so completed/canceled jobs cannot resurrect old terrain.
	for {
		select {
		case mesh := <-w.terrainMeshes:
			if flight, active := w.inflight[mesh.ID]; w.inflight == nil || (active && flight.ctx.Err() == nil) {
				w.savePending(mesh)
			}
		default:
			goto results
		}
	}
results:
	for {
		select {
		case mesh := <-w.results:
			flight, active := w.inflight[mesh.ID]
			if w.inflight == nil || (active && flight.ctx.Err() == nil) {
				w.savePending(mesh)
			} else {
				w.discardIncomplete(mesh.ID)
			}
			w.finishJob(mesh.ID, active && flight.ctx.Err() == nil)
		default:
			goto canceled
		}
	}
canceled:
	for {
		select {
		case id := <-w.canceled:
			if _, active := w.inflight[id]; active || (w.inflight == nil && w.working && w.activeID == id) {
				w.finishJob(id, false)
				w.discardIncomplete(id)
			}
		default:
			return
		}
	}
}

func (w *world) finishJob(id int64, completed bool) {
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

// Cancellation may leave an early terrain result without a vegetation result.
// Remove that provisional cache so revisiting the section schedules a rebuild.
func (w *world) discardIncomplete(id int64) {
	if u := w.parked[id]; u != nil {
		u.deallocate()
		delete(w.parked, id)
	}
	if mesh, ok := w.pending[id]; ok && mesh.TerrainOnly {
		delete(w.pending, id)
	}
	if w.upload != nil && w.upload.data.ID == id && w.upload.data.TerrainOnly {
		w.deferUpload()
		delete(w.pending, id)
	}
	if section := w.sections[id]; section != nil && section.mesh != nil && section.mesh.TerrainOnly {
		section.deallocate()
		delete(w.sections, id)
		w.revision++
	}
	if _, ok := w.collision[id]; ok {
		delete(w.collision, id)
		w.collisionRevision++
	}
}

func (w *world) needsForeground(mesh render.SectionMesh) bool {
	if mesh.LayerMask()&render.ForegroundLayer == 0 {
		return false
	}
	section := w.sections[mesh.ID]
	return section == nil || section.foreground == nil || section.renderWidth() != w.renderWidth() || !terrain.SameTerrain(section.geometry, mesh.Geometry)
}

func (w *world) uploadPriority(mesh render.SectionMesh) int {
	priority := 2 // Vegetation follows both rock layers.
	if w.needsForeground(mesh) {
		priority = 0
	} else if mesh.LayerMask()&render.BackgroundLayer != 0 {
		s := w.sections[mesh.ID]
		if s == nil || s.terrain == nil || s.backgroundWidth() != w.renderWidth() || s.mesh == nil || !sameGridMesh(s.mesh.Background, mesh.Background) {
			priority = 1
		}
	}
	if render.ValidViewport(w.viewport) {
		low, high := visibleSections(w.viewport.Y, w.viewport.Height)
		if mesh.ID < low || mesh.ID > high {
			priority += 3
		}
	}
	return priority
}

// Selection and preemption occur between draw batches. Parked GPU surfaces
// retain progress, including the supersampled antialiasing accumulator.
func (w *world) receive(g *Scene) {
	budget := uploadBudget{start: time.Now()}
	w.collect()
	var selected render.SectionMesh
	found := false
	choose := func(mesh render.SectionMesh) {
		if !meshVisible(mesh, w.viewport) {
			return
		}
		if !found || w.uploadPriority(mesh) < w.uploadPriority(selected) ||
			(w.uploadPriority(mesh) == w.uploadPriority(selected) && mesh.ID < selected.ID) {
			selected, found = mesh, true
		}
	}
	for _, mesh := range w.pending {
		choose(mesh)
	}
	for _, u := range w.parked {
		choose(u.data)
	}
	// Viewport bands precede contributing neighboring windows within a phase.
	if render.ValidViewport(w.viewport) {
		ids, _ := prefetch(w.viewport.Y, w.viewport.Height, w.viewport.Velocity)
		for _, id := range ids {
			if mesh, ok := w.pending[id]; ok && meshVisible(mesh, w.viewport) && (!found || w.uploadPriority(mesh) == w.uploadPriority(selected)) {
				selected, found = mesh, true
				break
			}
		}
	}
	if w.upload != nil && found && selected.ID != w.upload.data.ID && w.uploadPriority(selected) < w.uploadPriority(w.upload.data) {
		if w.parked == nil {
			w.parked = make(map[int64]*sectionUpload)
		}
		w.parked[w.upload.data.ID] = w.upload
		w.upload = nil
	}
	if w.upload == nil {
		if found {
			w.startUpload(selected)
		}
		return
	}
	u := w.upload
	if u.pixels == 0 {
		u.pixels = w.renderWidth()
	}
	if !u.initialized {
		g.applyStoredCuts(&u.data)
		u.needed = u.data.LayerMask()
		section := w.sections[u.data.ID]
		if section != nil {
			if section.foreground != nil && section.renderWidth() == u.pixels && terrain.SameTerrain(section.geometry, u.data.Geometry) {
				u.needed &^= render.ForegroundLayer
			}
			if section.terrain != nil && section.backgroundWidth() == u.pixels && section.mesh != nil && sameGridMesh(section.mesh.Background, u.data.Background) {
				u.needed &^= render.BackgroundLayer
			}
			if !section.vegetationPending && section.vegetationWidth() == u.pixels && section.mesh != nil && section.mesh.Geometry == u.data.Geometry {
				u.needed &^= render.VegetationLayer
			}
		}
		u.initialized = true
		u.stage = u.firstStage()
	}
	if u.stage == uploadDone {
		w.updateSectionSource(u)
		w.upload = nil
		return
	}
	if w.white == nil {
		w.white = ebiten.NewImage(1, 1)
		w.white.Fill(color.White)
	}
	top := terrain.SectionWindowTop(u.data.ID)
	for {
		switch u.stage {
		case uploadForegroundFaces:
			if !budget.available() {
				return
			}
			if u.foreground == nil {
				u.raster.Foreground = render.ScaleGridMesh(u.data.Foreground, u.pixels, 0)
				u.foreground = render.NewSectionImageAt(terrain.SectionHeight, u.pixels)
			}
			if !w.uploadFaces(g, u.foreground, u.raster.Foreground.Faces, top, true, &budget) {
				return
			}
		case uploadForegroundOutlines:
			if !w.uploadMeshes(u.foreground, u.raster.Foreground.Outlines, &budget) {
				return
			}
		case uploadForegroundPublish:
			section := w.sectionForUpload(u)
			changed := section.foreground == nil || !terrain.SameTerrain(section.geometry, u.data.Geometry)
			if section.foreground != nil {
				section.foreground.Deallocate()
			}
			section.foreground, u.foreground = u.foreground, nil
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
		case uploadBackgroundFaces:
			if !budget.available() {
				return
			}
			if u.img == nil {
				u.raster.Background = render.ScaleGridMesh(u.data.Background, u.pixels, -render.BackgroundRasterBounds(u.pixels).Min.X)
				u.img = render.NewBackgroundImageAt(u.pixels)
				u.img.Fill(color.Black)
			}
			if !w.uploadFaces(g, u.img, u.raster.Background.Faces, top, true, &budget) {
				return
			}
		case uploadBackgroundOutlines:
			if !w.uploadMeshes(u.img, u.raster.Background.Outlines, &budget) {
				return
			}
		case uploadBackgroundFade:
			if !budget.enter(u.stage) || !budget.take(0) {
				return
			}
			faded := render.NewBackgroundImageAt(u.pixels)
			faded.DrawRectShader(u.img.Bounds().Dx(), u.img.Bounds().Dy(), g.backgroundFade, &ebiten.DrawRectShaderOptions{
				Images: [4]*ebiten.Image{u.img}, Uniforms: map[string]any{"Pixels": float32(u.pixels), "MinX": float32(float64(render.BackgroundRasterBounds(u.pixels).Min.X) / float64(u.pixels))},
			})
			u.img.Deallocate()
			u.img = faded
		case uploadBackgroundPublish:
			section := w.sectionForUpload(u)
			if section.terrain != nil {
				section.terrain.Deallocate()
			}
			section.terrain, u.img = u.img, nil
			section.backgroundPixels = u.pixels
			w.updateSectionSource(u)
			u.needed &^= render.BackgroundLayer
			u.stage = u.firstStage()
			u.next = 0
			return
		case uploadVines:
			if !budget.available() {
				return
			}
			// Scale vegetation only once it is ready to upload.
			if u.next == 0 && u.vines == nil {
				plants := render.ScaleVegetationMesh(u.data, u.pixels)
				u.raster.Vines, u.raster.VinesBounds = plants.Vines, plants.VinesBounds
				u.raster.ForegroundVines, u.raster.ForegroundVinesBounds = plants.ForegroundVines, plants.ForegroundVinesBounds
				u.raster.Mushrooms, u.raster.MushroomsBounds = plants.Mushrooms, plants.MushroomsBounds
				if !plants.VinesBounds.Empty() {
					u.vines = render.NewVegetationImage(plants.VinesBounds)
				}
			}
			if u.vines != nil && !w.uploadMeshes(u.vines, u.raster.Vines, &budget) {
				return
			}
		case uploadVineFinish:
			if u.vines != nil && (!budget.enter(u.stage) || !budget.take(0)) {
				return
			}
			u.vines = g.finishVinesAt(u.vines, u.raster.VinesBounds, top, u.pixels)
			if u.data.Geometry != nil {
				render.EraseVineCutsAt(u.vines, u.raster.VinesBounds, top, u.data.Geometry.Vegetation.Cuts, u.pixels)
			}
		case uploadMushrooms:
			if !u.raster.MushroomsBounds.Empty() {
				if !budget.available() {
					return
				}
				if u.mushrooms == nil {
					u.mushrooms = render.NewVegetationImage(u.raster.MushroomsBounds)
				}
				if !w.uploadFaces(g, u.mushrooms, u.raster.Mushrooms, top, false, &budget) {
					return
				}
			}
		case uploadForegroundVines:
			if !u.raster.ForegroundVinesBounds.Empty() {
				if !budget.available() {
					return
				}
				if u.foregroundVines == nil {
					u.foregroundVines = render.NewVegetationImage(u.raster.ForegroundVinesBounds)
				}
				if !w.uploadMeshes(u.foregroundVines, u.raster.ForegroundVines, &budget) {
					return
				}
			}
			if u.data.Geometry != nil {
				render.EraseVineCutsAt(u.foregroundVines, u.raster.ForegroundVinesBounds, top, u.data.Geometry.Vegetation.Cuts, u.pixels)
			}
		case uploadPlantsPublish:
			section := w.sectionForUpload(u)
			for _, img := range []*ebiten.Image{section.vines, section.foregroundVines, section.mushrooms} {
				if img != nil {
					img.Deallocate()
				}
			}
			section.vines, section.foregroundVines, section.mushrooms = u.vines, u.foregroundVines, u.mushrooms
			u.vines, u.foregroundVines, u.mushrooms = nil, nil, nil
			section.vinesBounds, section.foregroundVinesBounds, section.mushroomsBounds = u.raster.VinesBounds, u.raster.ForegroundVinesBounds, u.raster.MushroomsBounds
			section.vegetationPixels = u.pixels
			section.vegetationPending = false
			w.updateSectionSource(u)
			w.upload = nil
			return
		}
		u.stage++
		u.next = 0
	}
}

func sameGridMesh(a, b render.GridMesh) bool {
	return len(a.Faces.Vertices) == len(b.Faces.Vertices) && (len(a.Faces.Vertices) == 0 || &a.Faces.Vertices[0] == &b.Faces.Vertices[0])
}
func (u *sectionUpload) firstStage() int {
	if u.needed&render.ForegroundLayer != 0 {
		return uploadForegroundFaces
	}
	if u.needed&render.BackgroundLayer != 0 {
		return uploadBackgroundFaces
	}
	if u.needed&render.VegetationLayer != 0 {
		return uploadVines
	}
	return uploadDone
}
func (w *world) sectionForUpload(u *sectionUpload) *worldSection {
	s := w.sections[u.data.ID]
	if s == nil {
		s = &worldSection{vegetationPending: true}
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
func (w *world) uploadFaces(g *Scene, dst *ebiten.Image, mesh render.TriangleMesh, top float64, material bool, budget *uploadBudget) bool {
	u := w.upload
	if len(mesh.Indices) == 0 {
		return true
	}
	if !budget.enter(u.stage) {
		return false
	}
	if u.surface == nil {
		size := dst.Bounds().Size()
		u.surface = ebiten.NewImageWithOptions(image.Rect(0, 0, size.X*2, size.Y*2), &ebiten.NewImageOptions{Unmanaged: true})
		u.faceNext = 0
	}
	if u.faceNext < len(mesh.Indices) {
		remaining := uploadIndicesPerTick - budget.indices
		end := min(u.faceNext+(remaining/3)*3, len(mesh.Indices))
		if end <= u.faceNext || !budget.take(end-u.faceNext) {
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
	if !budget.take(0) {
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
