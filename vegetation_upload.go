package infinicave

import (
	"image"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

type vegetationProduct uint8

const (
	backgroundVinesProduct vegetationProduct = iota
	websProduct
	mushroomsProduct
	foregroundVinesProduct
	vegetationProducts
)

type plantUpload struct {
	cursor uploadCursor
	done   bool
}

type plantPublication struct {
	source *render.SectionMesh // Immutable recipe for the displayed image.
	pixels int                 // Nonzero also records publication of an empty product.
	recipe *uint8
	reveal render.Reveal
}

func (s *worldSection) plantWidth(kind vegetationProduct) int {
	if pixels := s.plants[kind].pixels; pixels > 0 {
		return pixels
	}
	return s.vegetationWidth()
}

func (s *worldSection) plantReveal(kind vegetationProduct) *render.Reveal {
	return &s.plants[kind].reveal
}

// Cropped images have independent cursors and publication. An atlas wait or a
// long vine mesh only consumes that product's turn, not its siblings' progress.
func (w *world) advancePlants(g *Scene, u *sectionUpload, budget *uploadBudget) {
	if !u.plantsInitialized {
		for i := range u.plants {
			u.plants[i].done = true
		}
		w.addPlantUploads(u, u.needed&render.VegetationLayer)
		u.plantsInitialized = true
	}
	for range vegetationProducts {
		kind := vegetationProduct(u.plantNext)
		u.plantNext = (u.plantNext + 1) % int(vegetationProducts)
		p := &u.plants[kind]
		if p.done {
			continue
		}
		before := budget.draws
		if w.advancePlant(g, u, kind, &p.cursor, budget) {
			w.publishPlant(g, u, kind)
			p.done = true
			u.progress++
			break
		}
		if budget.draws > before {
			break
		}
	}
	for _, p := range u.plants {
		if !p.done {
			return
		}
	}
	s := w.sectionForUpload(u)
	s.updateVegetationReadiness(u.pixels)
	u.needed &^= render.VegetationLayer
	if u.stage == uploadVines {
		u.stage = u.firstStage()
	}
	if u.needed == 0 {
		w.upload = nil
	}
}

func (w *world) advancePlant(g *Scene, u *sectionUpload, kind vegetationProduct, cursor *uploadCursor, budget *uploadBudget) bool {
	top := terrain.SectionWindowTop(u.data.ID)
	var dst **ebiten.Image
	var bounds image.Rectangle
	switch kind {
	case backgroundVinesProduct:
		dst, bounds = &u.vines, u.raster.VinesBounds
	case websProduct:
		dst, bounds = &u.spiderWebs, u.raster.SpiderWebsBounds
	case mushroomsProduct:
		dst, bounds = &u.mushrooms, u.raster.MushroomsBounds
	case foregroundVinesProduct:
		dst, bounds = &u.foregroundVines, u.raster.ForegroundVinesBounds
	}
	if u.data.BakedInstances && (kind == websProduct && !render.HasLiveAttachments(u.raster.Webs, false) || kind == mushroomsProduct && !render.HasLiveAttachments(u.raster.Plants, false)) {
		if *dst != nil {
			(*dst).Deallocate()
			*dst = nil
		}
		return true
	}
	if bounds.Empty() {
		return true
	}
	if !budget.available() {
		return false
	}
	if *dst == nil {
		*dst = render.NewVegetationImage(bounds)
	}
	switch kind {
	case backgroundVinesProduct:
		if cursor.stage == uploadVines {
			if !w.uploadMeshes(cursor, *dst, u.raster.Vines, budget) {
				return false
			}
			cursor.stage = uploadVineFinish
		}
		if !budget.enter(cursor.stage) || !budget.takeRaster(0, bounds.Dx()*bounds.Dy()) {
			return false
		}
		*dst = g.finishVinesAt(*dst, bounds, top, u.pixels)
		return true
	case foregroundVinesProduct:
		return w.uploadMeshes(cursor, *dst, u.raster.ForegroundVines, budget)
	case websProduct:
		if u.data.BakedInstances {
			return w.uploadPlantInstances(g, cursor, *dst, u.raster.Webs, budget)
		}
		return w.uploadFaces(g, cursor, *dst, u.raster.SpiderWebs, top, false, budget)
	default:
		if u.data.BakedInstances {
			return w.uploadPlantInstances(g, cursor, *dst, u.raster.Plants, budget)
		}
		return w.uploadFaces(g, cursor, *dst, u.raster.Mushrooms, top, false, budget)
	}
}

func (w *world) publishPlant(g *Scene, u *sectionUpload, kind vegetationProduct) {
	s := w.sectionForUpload(u)
	newGeometry := s.geometry == nil && u.data.Geometry != nil
	var dst, src **ebiten.Image
	switch kind {
	case backgroundVinesProduct:
		dst, src = &s.vines, &u.vines
		s.vinesBounds = u.raster.VinesBounds
	case websProduct:
		dst, src = &s.spiderWebs, &u.spiderWebs
		s.spiderWebsBounds = u.raster.SpiderWebsBounds
	case mushroomsProduct:
		dst, src = &s.mushrooms, &u.mushrooms
		s.mushroomsBounds = u.raster.MushroomsBounds
	case foregroundVinesProduct:
		dst, src = &s.foregroundVines, &u.foregroundVines
		s.foregroundVinesBounds = u.raster.ForegroundVinesBounds
		g.eraseCuts(*src, s.foregroundVinesBounds, terrain.SectionWindowTop(u.data.ID), u.data.VisualCuts, u.pixels)
	}
	p := &s.plants[kind]
	if w.appearanceFade != nil && *dst == nil && *src != nil && p.pixels == 0 {
		p.reveal.Start(nil)
	}
	if *dst != nil {
		(*dst).Deallocate()
	}
	*dst, *src = *src, nil
	source := plantLayerSource(u.data, kind)
	p.source = &source
	p.pixels, p.recipe = u.pixels, u.data.PlantRecipes[kind]
	w.updateSectionSource(u)
	if newGeometry {
		w.revision++
	}
}

func (kind vegetationProduct) layer() render.MeshLayers { return render.BackgroundVinesLayer << kind }

func (s *worldSection) needsPlant(mesh render.SectionMesh, kind vegetationProduct, pixels int) bool {
	if mesh.LayerMask()&kind.layer() == 0 {
		return false
	}
	p := s.plants[kind]
	if recipe := mesh.PlantRecipes[kind]; recipe != nil {
		return p.pixels != pixels || p.recipe != recipe
	}
	// Legacy complete fixtures have no per-product recipe identity.
	return s.vegetationPending || s.plantWidth(kind) != pixels || s.mesh == nil || s.mesh.Geometry != mesh.Geometry
}

func (s *worldSection) updateVegetationReadiness(pixels int) {
	s.vegetationPending = true
	if s.mesh == nil || s.mesh.LayerMask()&render.VegetationLayer != render.VegetationLayer {
		return
	}
	for kind := vegetationProduct(0); kind < vegetationProducts; kind++ {
		p := s.plants[kind]
		if p.pixels != pixels || p.recipe != s.mesh.PlantRecipes[kind] {
			return
		}
	}
	s.vegetationPixels, s.vegetationPending = pixels, false
}

// Install only newly arrived recipes; existing cursors and AA surfaces keep
// their progress when another CPU generator finishes.
func (w *world) addPlantUploads(u *sectionUpload, mask render.MeshLayers) {
	data := u.data
	data.Layers = mask
	if w.disableBackground {
		data.Vines, data.VinesBounds = nil, image.Rectangle{}
	}
	plants := render.ScaleVegetationMesh(data, u.pixels)
	if mask&render.BackgroundVinesLayer != 0 {
		u.raster.Vines, u.raster.VinesBounds = plants.Vines, plants.VinesBounds
	}
	if mask&render.ForegroundVinesLayer != 0 {
		u.raster.ForegroundVines, u.raster.ForegroundVinesBounds = plants.ForegroundVines, plants.ForegroundVinesBounds
	}
	if mask&render.MushroomsLayer != 0 {
		u.raster.Mushrooms, u.raster.MushroomsBounds = plants.Mushrooms, plants.MushroomsBounds
		u.raster.Plants = plants.Plants
	}
	if mask&render.WebsLayer != 0 {
		u.raster.SpiderWebs, u.raster.SpiderWebsBounds = plants.SpiderWebs, plants.SpiderWebsBounds
		u.raster.Webs = plants.Webs
	}
	for kind, stage := range []int{uploadVines, uploadSpiderWebs, uploadMushrooms, uploadForegroundVines} {
		if mask&vegetationProduct(kind).layer() != 0 {
			u.plants[kind] = plantUpload{cursor: uploadCursor{stage: stage}}
		}
	}
}

// Retain only this plant's buffers and physical snapshot for displayed-image edits.
func plantLayerSource(mesh render.SectionMesh, kind vegetationProduct) render.SectionMesh {
	source := render.SectionMesh{ID: mesh.ID, Geometry: mesh.Geometry,
		VisualCuts: mesh.VisualCuts, AppliedCuts: mesh.AppliedCuts,
		BakedInstances: mesh.BakedInstances, Layers: kind.layer()}
	switch kind {
	case backgroundVinesProduct:
		source.Vines, source.VinesBounds = mesh.Vines, mesh.VinesBounds
	case foregroundVinesProduct:
		source.ForegroundVines, source.ForegroundVinesBounds = mesh.ForegroundVines, mesh.ForegroundVinesBounds
	case mushroomsProduct:
		source.Plants, source.PlantIndex, source.Mushrooms, source.MushroomsBounds = mesh.Plants, mesh.PlantIndex, mesh.Mushrooms, mesh.MushroomsBounds
	case websProduct:
		source.Webs, source.WebIndex, source.SpiderWebs, source.SpiderWebsBounds = mesh.Webs, mesh.WebIndex, mesh.SpiderWebs, mesh.SpiderWebsBounds
	}
	return source
}
