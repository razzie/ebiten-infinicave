package infinicave

import (
	"image"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Background faces, outlines, and material processing never wait for foreground.
func (w *world) advanceBackground(g *Scene, u *sectionUpload, budget *uploadBudget) {
	cursor := &u.backgroundCursor
	if cursor.stage == 0 {
		cursor.stage = uploadBackgroundFaces
	}
	top := terrain.SectionWindowTop(u.data.ID)
	for {
		switch cursor.stage {
		case uploadBackgroundFaces:
			if !budget.available() {
				return
			}
			if u.img == nil {
				u.raster.Background = render.ScaleGridMesh(u.data.Background, u.pixels, -render.BackgroundRasterBounds(u.pixels).Min.X)
				u.img = render.NewBackgroundImageAt(u.pixels)
				u.img.Fill(color.Black)
			}
			if !w.uploadFaces(g, cursor, u.img, u.raster.Background.Faces, top, true, budget) {
				return
			}
		case uploadBackgroundLighting:
			if g.background != nil {
				if !budget.available() {
					return
				}
				if u.backgroundLighting == nil {
					u.backgroundLighting = render.NewBackgroundImageAt(u.pixels)
				}
				if !w.uploadFaces(g, cursor, u.backgroundLighting, u.raster.Background.Faces, top, false, budget) {
					return
				}
			}
		case uploadBackgroundOutlines:
			if !w.uploadMeshes(cursor, u.img, u.raster.Background.Outlines, budget) {
				return
			}
		case uploadBackgroundFade:
			if !budget.enter(cursor.stage) || !budget.takeRaster(0, u.img.Bounds().Dx()*u.img.Bounds().Dy()) {
				return
			}
			faded := render.NewBackgroundImageAt(u.pixels)
			faded.DrawRectShader(u.img.Bounds().Dx(), u.img.Bounds().Dy(), g.backgroundFade, &ebiten.DrawRectShaderOptions{
				Images: [4]*ebiten.Image{u.img}, Uniforms: map[string]any{"Pixels": float32(u.pixels), "MinX": float32(float64(render.BackgroundRasterBounds(u.pixels).Min.X) / float64(u.pixels))},
			})
			u.img.Deallocate()
			u.img = faded
		case uploadBackgroundPublish:
			u.progress++
			section := w.sectionForUpload(u)
			if w.appearanceFade != nil && section.terrain == nil {
				section.backgroundReveal.Start(nil)
			}
			if section.terrain != nil {
				section.terrain.Deallocate()
			}
			section.terrain, u.img = u.img, nil
			if section.backgroundLighting != nil {
				section.backgroundLighting.Deallocate()
			}
			section.backgroundLighting, u.backgroundLighting = u.backgroundLighting, nil
			section.backgroundPixels = u.pixels
			w.updateSectionSource(u)
			u.needed &^= render.BackgroundLayer
			if u.needed&render.ForegroundLayer == 0 {
				u.stage = u.firstStage()
			}
			return

		}
		cursor.stage++
		cursor.next = 0
	}
}

// Grass shares the immutable foreground recipe, but has its own upload and
// publication record. This bit belongs only to the GPU scheduler.
const grassUploadLayer render.MeshLayers = 1 << 6

func (s *worldSection) grassWidth() int {
	if s.grassPixels > 0 {
		return s.grassPixels
	}
	return s.renderWidth()
}

func (s *worldSection) needsGrass(mesh render.SectionMesh, pixels int) bool {
	if s.grassPending || s.grassWidth() != pixels {
		return true
	}
	if s.grassSource != nil {
		return !sameGridMesh(s.grassSource.Foreground, mesh.Foreground)
	}
	// Legacy/manual caches without a publication record can have known-empty grass.
	return len(mesh.Foreground.Grass.Indices) > 0 || render.HasLiveAttachments(mesh.Foreground.Growth, true)
}

func (w *world) advanceGrass(g *Scene, u *sectionUpload, budget *uploadBudget) {
	data := &u.data
	if u.grassData != nil {
		data = u.grassData
	}
	cursor := &u.grassCursor
	if !u.grassInitialized {
		grid := render.MaskForeground(data.Foreground, render.GrassSpan)
		grid.Faces, grid.Outlines = render.TriangleMesh{}, nil
		u.grassRaster = render.ScaleGridMesh(grid, u.pixels, 0)
		cursor.stage = uploadGrass
		u.grassInitialized = true
	}
	if !u.grassFacesDone {
		mesh := u.grassRaster.Grass
		if len(mesh.Indices) > 0 {
			if !budget.available() {
				return
			}
			if u.grass == nil {
				u.grass = render.NewSectionImageAt(terrain.SectionHeight, u.pixels)
			}
			if !w.uploadFaces(g, cursor, u.grass, mesh, terrain.SectionWindowTop(data.ID), true, budget) {
				return
			}
		}
		u.grassFacesDone = true
	}
	if render.HasLiveAttachments(u.grassRaster.Growth, true) {
		if !budget.available() {
			return
		}
		if u.grass == nil {
			u.grass = render.NewSectionImageAt(terrain.SectionHeight, u.pixels)
		}
		if !w.uploadAttachments(g, cursor, u.grass, u.grassRaster.Growth, true, false, budget) {
			return
		}
	}
	if len(data.VisualCuts) > 0 && len(data.Foreground.Grass.Indices) > 0 {
		var regions []geom.Bounds
		for _, cut := range data.VisualCuts {
			regions = append(regions, geom.Bounds{Min: cut.Min, Max: cut.Max})
		}
		g.replayGridRegions(&worldSection{grass: u.grass, mesh: data, pixels: u.pixels}, data.ID, regions, true)
	}
	s := w.sectionForUpload(u)
	newGeometry := s.geometry == nil && u.data.Geometry != nil
	if w.appearanceFade != nil && s.grass == nil && u.grass != nil && s.grassPixels == 0 {
		s.grassReveal.Start(nil)
	}
	if s.grassPixels != 0 && s.grassWidth() != u.pixels {
		s.grassReveal.Clear()
	}
	if s.grass != nil {
		s.grass.Deallocate()
	}
	s.grass, u.grass = u.grass, nil
	source := *data
	s.grassSource = &source
	s.grassPixels, s.grassPending = u.pixels, false
	w.updateSectionSource(u)
	if newGeometry {
		w.revision++
	}
	u.progress++
	u.needed &^= grassUploadLayer
	if u.needed&render.ForegroundLayer == 0 {
		u.stage = u.firstStage()
	}
}

type growthProduct uint8

const (
	underGrowth growthProduct = iota
	surfaceGrowth
	growthProducts
)
const underGrowthUploadLayer render.MeshLayers = 1 << 7
const surfaceGrowthUploadLayer render.MeshLayers = 1 << 8

type growthUpload struct {
	cursor      uploadCursor
	image       *ebiten.Image
	raster      render.GridMesh
	initialized bool
}

type growthPublication struct {
	image  *ebiten.Image
	source *render.SectionMesh
	pixels int // Nonzero records empty products too.
	reveal render.Reveal
}

func (kind growthProduct) layer() render.MeshLayers { return underGrowthUploadLayer << kind }
func (kind growthProduct) span() render.ForegroundSpan {
	if kind == underGrowth {
		return render.UnderGrowthSpan
	}
	return render.SurfaceGrowthSpan
}
func (u *sectionUpload) detailData() *render.SectionMesh {
	if u.grassData != nil {
		return u.grassData
	}
	return &u.data
}
func (s *worldSection) needsGrowth(mesh render.SectionMesh, kind growthProduct, pixels int) bool {
	if mesh.ForegroundBaseOnly || mesh.LayerMask()&render.ForegroundLayer == 0 {
		return false
	}
	p := &s.growth[kind]
	return p.pixels != pixels || p.source == nil || !sameGridMesh(p.source.Foreground, mesh.Foreground)
}
func (s *worldSection) growthReady(pixels int) bool {
	for _, p := range s.growth {
		if p.pixels != pixels {
			return false
		}
	}
	return true
}
func (p *growthUpload) deallocate() {
	if p.image != nil {
		p.image.Deallocate()
		p.image = nil
	}
	if p.cursor.surface != nil {
		p.cursor.surface.Deallocate()
		p.cursor.surface = nil
	}
}

func (w *world) advanceGrowth(g *Scene, u *sectionUpload, kind growthProduct, budget *uploadBudget) {
	data := u.detailData()
	p := &u.growth[kind]
	if !p.initialized {
		grid := render.MaskForeground(data.Foreground, kind.span())
		grid.Faces, grid.Outlines, grid.Grass = render.TriangleMesh{}, nil, render.TriangleMesh{}
		p.raster = render.ScaleGridMesh(grid, u.pixels, 0)
		p.cursor.stage = uploadGrowthProduct
		p.initialized = true
	}
	if render.HasForegroundSpan(p.raster, kind.span()) {
		if !budget.available() {
			return
		}
		if p.image == nil {
			p.image = render.NewSectionImageAt(terrain.SectionHeight, u.pixels)
		}
		if !w.uploadAttachments(g, &p.cursor, p.image, p.raster.Growth, false, kind == underGrowth, budget) {
			return
		}
	}
	if len(data.VisualCuts) > 0 && p.image != nil {
		var regions []geom.Bounds
		for _, cut := range data.VisualCuts {
			regions = append(regions, geom.Bounds{Min: cut.Min, Max: cut.Max})
		}
		grid := render.MaskForeground(data.Foreground, kind.span())
		g.replayForegroundAttachments(p.image, grid, data.ID, u.pixels, regions, kind == underGrowth, data.VisualCuts, nil)
	}
	s := w.sectionForUpload(u)
	published := &s.growth[kind]
	if w.appearanceFade != nil && published.pixels == 0 && p.image != nil {
		published.reveal.Start(nil)
	}
	if published.pixels > 0 && published.pixels != u.pixels {
		published.reveal.Clear()
	}
	if published.image != nil {
		published.image.Deallocate()
	}
	published.image, p.image = p.image, nil
	source := *data
	source.Foreground = render.MaskForeground(data.Foreground, kind.span())
	published.source, published.pixels = &source, u.pixels
	newGeometry := s.geometry == nil && u.data.Geometry != nil
	w.updateSectionSource(u)
	if newGeometry {
		w.revision++
	}
	u.progress++
	u.needed &^= kind.layer()
	if u.needed&render.ForegroundLayer == 0 {
		u.stage = u.firstStage()
	}
}

// During the structural reveal, compose the whole ordered span first so
// overlapping structural pixels fade uniformly. One reusable scratch image
// serves every section; normal fully revealed drawing needs no scratch pass.
func (w *world) drawRockSpan(dst *ebiten.Image, s *worldSection, transform ebiten.GeoM) {
	progress := s.foregroundReveal.Progress()
	if w.appearanceFade == nil || progress >= 1 {
		w.drawGrowthAt(dst, s, underGrowth, s.renderWidth(), transform)
		w.appearanceFade.DrawReveal(dst, s.foreground, nil, 1, transform)
		w.drawGrowthAt(dst, s, surfaceGrowth, s.renderWidth(), transform)
		if s.trim != nil {
			w.appearanceFade.DrawReveal(dst, s.trim, nil, 1, transform)
		}
		return
	}
	pixels := s.renderWidth()
	if w.rockScratch == nil || w.rockScratch.Bounds().Dx() != pixels {
		if w.rockScratch != nil {
			w.rockScratch.Deallocate()
		}
		w.rockScratch = render.NewSectionImageAt(terrain.SectionHeight, pixels)
	}
	span := w.rockScratch
	span.Clear()
	w.drawGrowthAt(span, s, underGrowth, pixels, ebiten.GeoM{})
	span.DrawImage(s.foreground, nil)
	w.drawGrowthAt(span, s, surfaceGrowth, pixels, ebiten.GeoM{})
	if s.trim != nil {
		span.DrawImage(s.trim, nil)
	}
	w.appearanceFade.DrawReveal(dst, span, nil, progress, transform)
}

func (w *world) drawGrowthAt(dst *ebiten.Image, s *worldSection, kind growthProduct, pixels int, transform ebiten.GeoM) {
	p := &s.growth[kind]
	if p.image == nil {
		return
	}
	var placement ebiten.GeoM
	placement.Scale(float64(pixels)/float64(p.pixels), float64(pixels)/float64(p.pixels))
	placement.Concat(transform)
	w.appearanceFade.DrawReveal(dst, p.image, nil, p.reveal.Progress(), placement)
}

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

func (w *world) prepareAtlas(kind render.AtlasKind, budget *uploadBudget) bool {
	if kind == render.NoAtlas {
		return true
	}
	if w.atlases == nil {
		w.atlases = &render.AtlasCache{Antialiasing: w.antialiasing}
	}
	return w.atlases.Prepare(kind, func(indices int) bool { return budget.take(indices) })
}

func (g *Scene) drawAttachmentMesh(dst *ebiten.Image, mesh render.TriangleMesh) {
	if dst == nil || len(mesh.Indices) == 0 {
		return
	}
	if mesh.Texture == render.NoAtlas {
		mesh.DrawWithAntialiasing(dst, g.world.white, g.antialiasing.GeometryAA())
		return
	}
	if g.world.atlases == nil {
		g.world.atlases = &render.AtlasCache{Antialiasing: g.antialiasing}
	}
	g.world.atlases.Prepare(mesh.Texture, func(int) bool { return true })
	mesh.DrawWithAntialiasing(dst, g.world.atlases.Image(mesh.Texture), g.antialiasing.GeometryAA())
}

func (w *world) uploadAttachments(g *Scene, u *uploadCursor, dst *ebiten.Image, instances []render.Attachment, grass, underlay bool, budget *uploadBudget) bool {
	if len(instances) == 0 {
		return true
	}
	if !budget.enter(u.stage) {
		return false
	}
	for u.next < len(instances) {
		if !budget.available() {
			return false
		}
		mesh, next := render.AttachmentBatch(instances, u.next, grass, underlay, 16384)
		if !w.prepareAtlas(mesh.Texture, budget) {
			return false
		}
		if len(mesh.Indices) > 0 && !budget.take(len(mesh.Indices)) {
			return false
		}
		g.drawAttachmentMesh(dst, mesh)
		u.next = next
	}
	return true
}

func (w *world) uploadPlantInstances(g *Scene, u *uploadCursor, dst *ebiten.Image, instances []render.Attachment, budget *uploadBudget) bool {
	if !budget.enter(u.stage) {
		return false
	}
	for u.next < len(instances) {
		a := instances[u.next]
		if a.Dead {
			u.next++
			u.instancePart = 0
			continue
		}
		mesh := a.Mesh
		if u.instancePart == 1 {
			mesh = a.Extra
		}
		if len(mesh.Indices) > 0 {
			if !w.prepareAtlas(mesh.Texture, budget) || !budget.take(len(mesh.Indices)) {
				return false
			}
			g.drawAttachmentMesh(dst, mesh)
		}
		if u.instancePart == 0 && len(a.Extra.Indices) > 0 {
			u.instancePart = 1
		} else {
			u.instancePart = 0
			u.next++
		}
	}
	return true
}

// NewShader prepares Kage, but the driver compiles its GPU program on first
// use. Submit transparent one-pixel draws while CPU generation is underway,
// rather than delaying the first completed terrain and vine uploads.
func (g *Scene) warmMaterials(dst *ebiten.Image) {
	if g.materialsWarm {
		return
	}
	g.materialsWarm = true
	if g.decorationBurn != nil {
		g.decorationBurn.Warm(dst)
	}
	if g.bats != nil {
		g.bats.Warm(dst)
	}
	mesh := render.TriangleMesh{
		Vertices: []ebiten.Vertex{{DstX: 0, DstY: 0}, {DstX: 1, DstY: 0}, {DstX: 0, DstY: 1}},
		Indices:  []uint32{0, 1, 2},
	}
	if g.material != nil {
		g.drawGridFacesWithAA(dst, mesh, 0, false)
	}
	if g.vineMaterial != nil {
		blank := ebiten.NewImage(1, 1)
		defer blank.Deallocate()
		dst.DrawRectShader(1, 1, g.vineMaterial, &ebiten.DrawRectShaderOptions{
			Images:   [4]*ebiten.Image{blank},
			Uniforms: map[string]any{"Texture": float32(g.texture / 8), "Offset": []float32{0, 0}, "Scale": float32(1)},
		})
	}
}
