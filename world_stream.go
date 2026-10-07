package infinicave

import (
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
	w.pending[data.ID] = data
}

// Abandon partial GPU work, retaining CPU meshes and already published images.
// Rapid resize events therefore coalesce into work at the latest resolution.
func (w *world) deferUpload() {
	u := w.upload
	if u == nil {
		return
	}
	w.savePending(u.data)
	if u.img != nil {
		u.img.Deallocate()
	}
	if u.foreground != nil {
		u.foreground.Deallocate()
	}
	if u.vines != nil {
		u.vines.Deallocate()
	}
	if u.foregroundVines != nil {
		u.foregroundVines.Deallocate()
	}
	if u.mushrooms != nil {
		u.mushrooms.Deallocate()
	}
	if section := w.sections[u.data.ID]; section != nil {
		data := u.data
		section.mesh = &data
	}
	w.upload = nil
}

func (w *world) startUpload(data render.SectionMesh) {
	w.upload = &sectionUpload{data: data, pixels: w.renderWidth()}
	delete(w.pending, data.ID)
}

// sectionUpload spreads one section's GPU work over several frames.
type sectionUpload struct {
	data            render.SectionMesh
	raster          render.SectionMesh
	pixels          int
	img             *ebiten.Image
	foreground      *ebiten.Image
	vines           *ebiten.Image
	foregroundVines *ebiten.Image
	mushrooms       *ebiten.Image
	stage           int
	next            int
}

// Limit draw submissions per tick as well as separating the large layer
// uploads. CPU tessellation has already finished before a result arrives.
const uploadDrawsPerTick = 16

const uploadTimePerTick = 2 * time.Millisecond

const uploadIndicesPerTick = 128 * 1024

func (w *world) uploadMeshes(dst *ebiten.Image, meshes []render.TriangleMesh) bool {
	u := w.upload
	end := min(u.next+uploadDrawsPerTick, len(meshes))
	start := time.Now()
	indices := 0
	for ; u.next < end; u.next++ {
		// Always make progress, even when a single batch exceeds the budget.
		if indices > 0 && (indices+len(meshes[u.next].Indices) > uploadIndicesPerTick || time.Since(start) >= uploadTimePerTick) {
			break
		}
		meshes[u.next].Draw(dst, w.white)
		indices += len(meshes[u.next].Indices)
	}
	return u.next == len(meshes)
}

// GPU resources stay on the game thread. Publish complete terrain/collision
// first, then add vegetation together once all its layers have finished.
func (w *world) receive(g *Scene) {
	if w.upload == nil {
		select {
		case data := <-w.results:
			w.working = false
			if !meshVisible(data, w.viewport) {
				w.savePending(data)
				return
			}
			w.startUpload(data)
		default:
		}
		return
	}
	u := w.upload
	if u.pixels == 0 { // Manually constructed uploads use the default scale.
		u.pixels = w.renderWidth()
	}
	if u.stage == 0 {
		g.applyStoredCuts(&u.data)
		u.raster = render.ScaleSectionMesh(u.data, u.pixels)
	}
	data := &u.raster
	top := terrain.SectionWindowTop(u.data.ID)
	if w.white == nil {
		w.white = ebiten.NewImage(1, 1)
		w.white.Fill(color.White)
	}
	switch u.stage {
	case 0:
		u.img = render.NewBackgroundImageAt(u.pixels)
		u.img.Fill(color.Black)
		g.drawGridFaces(u.img, data.Background.Faces, top)
	case 1:
		if !w.uploadMeshes(u.img, data.Background.Outlines) {
			return
		}
		// Fade the complete layer, including black pockets and outlines, once
		// per raster upload. Foreground and collision retain the original width.
		faded := render.NewBackgroundImageAt(u.pixels)
		faded.DrawRectShader(u.img.Bounds().Dx(), u.img.Bounds().Dy(), g.backgroundFade, &ebiten.DrawRectShaderOptions{
			Images:   [4]*ebiten.Image{u.img},
			Uniforms: map[string]any{"Pixels": float32(u.pixels), "MinX": float32(float64(render.BackgroundRasterBounds(u.pixels).Min.X) / float64(u.pixels))},
		})
		u.img.Deallocate()
		u.img = faded
	case 2:
		u.foreground = render.NewSectionImageAt(terrain.SectionHeight, u.pixels)
		g.drawGridFaces(u.foreground, data.Foreground.Faces, top)
	case 3:
		if !w.uploadMeshes(u.foreground, data.Foreground.Outlines) {
			return
		}
	case 4:
		geometryChanged := true
		if previous := w.sections[u.data.ID]; previous != nil {
			geometryChanged = previous.geometry != u.data.Geometry
			previous.deallocate()
		}
		source := u.data
		w.sections[u.data.ID] = &worldSection{terrain: u.img, foreground: u.foreground,
			geometry: u.data.Geometry, vegetationPending: true, mesh: &source, pixels: u.pixels}
		delete(w.collision, u.data.ID)
		u.img, u.foreground = nil, nil // ownership moved to the cache
		if geometryChanged {
			w.revision++
		}
	case 5:
		if data.VinesBounds.Empty() {
			break
		}
		if u.vines == nil {
			u.vines = render.NewVegetationImage(data.VinesBounds)
		}
		if !w.uploadMeshes(u.vines, data.Vines) {
			return
		}
	case 6:
		u.vines = g.finishVinesAt(u.vines, data.VinesBounds, top, u.pixels)
		if u.data.Geometry != nil {
			render.EraseVineCutsAt(u.vines, data.VinesBounds, top, u.data.Geometry.Vegetation.Cuts, u.pixels)
		}
		if !data.MushroomsBounds.Empty() {
			u.mushrooms = render.NewVegetationImage(data.MushroomsBounds)
			data.Mushrooms.Draw(u.mushrooms, w.white)
		}
	case 7:
		if !data.ForegroundVinesBounds.Empty() {
			if u.foregroundVines == nil {
				u.foregroundVines = render.NewVegetationImage(data.ForegroundVinesBounds)
			}
			if !w.uploadMeshes(u.foregroundVines, data.ForegroundVines) {
				return
			}
		}
		if u.data.Geometry != nil {
			render.EraseVineCutsAt(u.foregroundVines, data.ForegroundVinesBounds, top, u.data.Geometry.Vegetation.Cuts, u.pixels)
		}
	default:
		section := w.sections[u.data.ID]
		section.vines, section.foregroundVines, section.mushrooms = u.vines, u.foregroundVines, u.mushrooms
		section.vinesBounds, section.foregroundVinesBounds, section.mushroomsBounds = data.VinesBounds, data.ForegroundVinesBounds, data.MushroomsBounds
		source := u.data
		section.mesh = &source
		section.vegetationPending = false
		w.upload = nil
		return
	}
	u.stage++
	u.next = 0
}
