package infinicave

import (
	"image"
	"image/color"
	"math"
	"slices"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Keep each cut's immutable physical snapshot so several calls before a draw
// replay in order, including decoration burns and overlapping cut rims.
type carveSectionImageEdit struct {
	id       int64
	section  *worldSection
	geometry *terrain.Geometry
	removals terrain.DecorationChanges
}

type carveImageEdit struct {
	cut      terrain.RockCut
	sections []carveSectionImageEdit
}

// Flush before uploads can replace published images, or drawing can use them.
// Reset and Close discard this world-owned queue along with its resources.
func (g *Scene) flushCarveImages() {
	w := g.world
	queued := w.carveImages
	w.carveImages = nil
	for _, edit := range queued {
		for _, s := range edit.sections {
			if w.sections[s.id] == s.section {
				geometry := s.geometry.WithoutGuides(w.retiredGuides)
				g.editSectionImages(s.section, s.id, edit.cut, geometry, s.removals)
			}
		}
		g.carveUpload(edit.cut)
	}
}

func (g *Scene) finishVinesAt(img *ebiten.Image, bounds image.Rectangle, top float64, pixels int) *ebiten.Image {
	if img == nil || g.vineMaterial == nil {
		return img
	}
	finished := render.NewVegetationImage(bounds)
	finished.DrawRectShader(bounds.Dx(), bounds.Dy(), g.vineMaterial, &ebiten.DrawRectShaderOptions{
		Images: [4]*ebiten.Image{img},
		Uniforms: map[string]any{
			"Offset":  []float32{float32(bounds.Min.X), float32(top*float64(pixels)) + float32(bounds.Min.Y)},
			"Scale":   float32(render.RasterPixelsPerUnit) / float32(pixels),
			"Texture": float32(g.texture / 8),
		},
	})
	img.Deallocate()
	return finished
}

// Original meshes are immutable bake recipes. Edits only filter attachment
// instances and retain masks; vine meshes keep their original material/taper.
type meshImageEdits struct {
	growth, plants, webs               []int
	rock, grass, mushrooms, spiderWebs []geom.Bounds
}

func attachmentDirty(source []render.Attachment, removed []int, top float64, grass bool) []geom.Bounds {
	var regions []geom.Bounds
	for _, i := range removed {
		a := source[i]
		if (a.Kind == render.GrassAttachment) == grass {
			pad := V{X: .012, Y: .012}
			regions = append(regions, geom.Bounds{Min: a.Min.Add(V{Y: top}).Sub(pad), Max: a.Max.Add(V{Y: top}).Add(pad)})
		}
	}
	return regions
}

func editMeshImages(mesh *render.SectionMesh, cut terrain.RockCut, removals terrain.DecorationChanges) meshImageEdits {
	found := false
	for _, old := range mesh.VisualCuts {
		if old.Min == cut.Min && old.Max == cut.Max && sameCutPolygon(old.Poly, cut.Poly) {
			found = true
			break
		}
	}
	if found {
		return meshImageEdits{}
	}
	mesh.VisualCuts = append(append([]terrain.RockCut(nil), mesh.VisualCuts...), cut)
	top := terrain.SectionTop(mesh.ID)
	var edits meshImageEdits
	mesh.Foreground.Growth, edits.growth = render.FilterAttachmentsIndexedWithChanges(mesh.Foreground.Growth, mesh.Foreground.GrowthIndex, []terrain.RockCut{cut}, top)
	mesh.Plants, edits.plants = render.FilterAttachmentsByID(mesh.Plants, mesh.PlantIndex, removals.Mushrooms)
	mesh.Webs, edits.webs = render.FilterAttachmentsByID(mesh.Webs, mesh.WebIndex, removals.Webs)
	edits.rock = attachmentDirty(mesh.Foreground.Growth, edits.growth, top, false)
	edits.grass = attachmentDirty(mesh.Foreground.Growth, edits.growth, top, true)
	edits.mushrooms = attachmentDirty(mesh.Plants, edits.plants, top, false)
	edits.spiderWebs = attachmentDirty(mesh.Webs, edits.webs, top, false)
	return edits
}

func sameCutPolygon(a, b []V) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (g *Scene) applyStoredCuts(mesh *render.SectionMesh) {
	for _, i := range g.world.cutsForSection(mesh.ID, mesh.AppliedCuts) {
		cut := g.world.cuts[i].expand()
		geometry, _, _ := cut.Geometry(mesh.ID, mesh.Geometry, g.collisionTolerance)
		geometry, _, removals := cut.PlantsWithChanges(geometry)
		mesh.Geometry = geometry
		editMeshImages(mesh, cut, removals)
	}
	mesh.AppliedCuts = uint64(len(g.world.cuts))
	mesh.Geometry = mesh.Geometry.WithoutGuides(g.world.retiredGuides)
}

// Dirty regions are reconstructed from original recipes before masking, so
// repeated/overlapping antialiased cuts cannot progressively erode their edges.
func (g *Scene) editSectionImages(section *worldSection, id int64, cut terrain.RockCut, geometry *terrain.Geometry, removals terrain.DecorationChanges) {
	if section.mesh == nil || cut.Max.Y <= terrain.SectionTop(id)+terrain.GenerationMinY || cut.Min.Y >= terrain.SectionTop(id)+2*SectionHeight {
		return
	}
	mesh := *section.mesh
	oldCuts := mesh.VisualCuts
	edits := editMeshImages(&mesh, cut, removals)
	grassEdits := edits
	mesh.Geometry = geometry
	mesh.AppliedCuts = uint64(len(g.world.cuts))
	section.mesh = &mesh
	// A completed plant recipe may arrive while the old rock base is still
	// displayed. Replay and burn only the rock recipe that supplied its pixels.
	displayed := *section
	displayed.geometry = geometry
	if section.rockSource != nil {
		rock, rockEdits := g.editLayerSource(*section.rockSource, render.ForegroundLayer, cut, removals)
		section.rockSource = &rock
		displayedMesh := mesh
		displayedMesh.Foreground = rock.Foreground
		displayed.mesh = &displayedMesh
		displayed.geometry = rock.Geometry
		edits.growth, edits.rock = rockEdits.growth, rockEdits.rock
	}
	// Grass may have published from a newer recipe or at a different width.
	grassDisplayed := *section
	if section.grassSource != nil {
		var grass render.SectionMesh
		grass, grassEdits = g.editLayerSource(*section.grassSource, grassUploadLayer, cut, removals)
		section.grassSource = &grass
		grassDisplayed.mesh = &grass
	}
	// CPU query snapshots can advance while old plant images remain displayed.
	// Replay each fallback against its own placements and support snapshot.
	plantMesh := *displayed.mesh
	vineCuts := mesh.VisualCuts
	for kind := vegetationProduct(0); kind < vegetationProducts; kind++ {
		published := &section.plants[kind]
		if published.source == nil {
			continue
		}
		source, changes := g.editLayerSource(*published.source, kind.layer(), cut, removals)
		published.source = &source
		switch kind {
		case mushroomsProduct:
			plantMesh.Plants, plantMesh.PlantIndex, plantMesh.MushroomsBounds = source.Plants, source.PlantIndex, source.MushroomsBounds
			edits.plants, edits.mushrooms = changes.plants, changes.mushrooms
		case websProduct:
			plantMesh.Webs, plantMesh.WebIndex, plantMesh.SpiderWebsBounds = source.Webs, source.WebIndex, source.SpiderWebsBounds
			edits.webs, edits.spiderWebs = changes.webs, changes.spiderWebs
		case foregroundVinesProduct:
			plantMesh.ForegroundVines, plantMesh.ForegroundVinesBounds = source.ForegroundVines, source.ForegroundVinesBounds
			vineCuts = source.VisualCuts
		}
	}
	displayed.mesh = &plantMesh
	grassDisplayed.pixels = section.grassWidth()
	grassDisplayed.foreground, grassDisplayed.foregroundVines = nil, nil
	grassDisplayed.mushrooms, grassDisplayed.spiderWebs = nil, nil
	displayed.grass = nil
	if g.world.white == nil {
		g.world.white = ebiten.NewImage(1, 1)
		g.world.white.Fill(color.White)
	}
	g.captureDecorationBurns(&displayed, id, cut, oldCuts, edits)
	g.captureDecorationBurns(&grassDisplayed, id, cut, oldCuts, grassEdits)
	// Includes bevels, new rims, and filter reach. Snap native regions to small
	// tiles so attachment overlap and raster antialiasing have stable origins.
	dirty := geom.Bounds{Min: cut.Min.Sub(V{X: .012, Y: .012}), Max: cut.Max.Add(V{X: .012, Y: .012})}
	overlap := false
	for _, old := range oldCuts {
		if cut.Max.X+.02 > old.Min.X && cut.Min.X-.02 < old.Max.X && cut.Max.Y+.02 > old.Min.Y && cut.Min.Y-.02 < old.Max.Y {
			overlap = true
			break
		}
	}
	pixels := section.renderWidth()
	bounds := image.Rect(0, pixels, pixels, 2*pixels)
	top := terrain.SectionWindowTop(id)
	if previous := section.foregroundReveal.Previous; previous != nil {
		g.eraseCuts(previous, bounds, top, []terrain.RockCut{cut}, pixels)
	}
	g.editGrowthImages(section, id, cut, geometry, removals, dirty, overlap)
	if overlap || len(edits.rock) > 0 {
		g.replayGridRegions(&displayed, id, append(edits.rock, dirty), false)
	} else {
		// The common isolated cut operates directly on the finished image.
		g.eraseCuts(section.foreground, bounds, top, []terrain.RockCut{cut}, pixels)
		rect := dirtyRaster(bounds, top, pixels, dirty, section.foreground)
		if !rect.Empty() && !section.foregroundSplit {
			g.drawShapeRims(section.foreground.SubImage(rect).(*ebiten.Image), displayed.geometry, pixels, rect)
		}
	}
	if section.foregroundSplit && section.foreground != nil {
		if section.trim == nil {
			section.trim = render.NewSectionImageAt(terrain.SectionHeight, pixels)
		}
		displayed.trim = section.trim
		trim := render.MaskForeground(displayed.mesh.Foreground, render.StructuralTopSpan)
		g.replayForegroundAttachments(section.trim, trim, id, pixels, append(edits.rock, dirty), false, displayed.mesh.VisualCuts, displayed.geometry)
	}
	if section.grass != nil {
		grassPixels := section.grassWidth()
		if len(grassEdits.grass) > 0 || overlap {
			g.replayGridRegions(&grassDisplayed, id, append(grassEdits.grass, dirty), true)
		} else {
			g.eraseCuts(section.grass, image.Rect(0, grassPixels, grassPixels, 2*grassPixels), top, []terrain.RockCut{cut}, grassPixels)
		}
	}
	if overlap {
		g.replayForegroundVineRegion(section.foregroundVines, plantMesh.ForegroundVines, plantMesh.ForegroundVinesBounds, section.foregroundVinesBounds, id, section.plantWidth(foregroundVinesProduct), dirty, vineCuts)
	} else {
		g.eraseCuts(section.foregroundVines, section.foregroundVinesBounds, top, []terrain.RockCut{cut}, section.plantWidth(foregroundVinesProduct))
	}
	if len(edits.plants) > 0 {
		g.replayAttachmentRegions(section.mushrooms, plantMesh.Plants, plantMesh.PlantIndex, plantMesh.MushroomsBounds, section.mushroomsBounds, id, section.plantWidth(mushroomsProduct), edits.mushrooms)
	}
	if len(edits.webs) > 0 {
		g.replayAttachmentRegions(section.spiderWebs, plantMesh.Webs, plantMesh.WebIndex, plantMesh.SpiderWebsBounds, section.spiderWebsBounds, id, section.plantWidth(websProduct), edits.spiderWebs)
	}
	if len(grassEdits.grass) > 0 && len(grassDisplayed.mesh.Foreground.Grass.Indices) == 0 && !render.HasLiveAttachments(grassDisplayed.mesh.Foreground.Growth, true) && section.grass != nil {
		section.grass.Deallocate()
		section.grass = nil
	}
	if mesh.BakedInstances {
		if len(edits.plants) > 0 && !render.HasLiveAttachments(plantMesh.Plants, false) && section.mushrooms != nil {
			section.mushrooms.Deallocate()
			section.mushrooms = nil
		}
		if len(edits.webs) > 0 && !render.HasLiveAttachments(plantMesh.Webs, false) && section.spiderWebs != nil {
			section.spiderWebs.Deallocate()
			section.spiderWebs = nil
		}
	}
}

func dirtyRaster(bounds image.Rectangle, top float64, pixels int, dirty geom.Bounds, dst *ebiten.Image) image.Rectangle {
	if dst == nil {
		return image.Rectangle{}
	}
	loX := int(math.Floor(dirty.Min.X*float64(pixels))) - bounds.Min.X
	hiX := int(math.Ceil(dirty.Max.X*float64(pixels))) - bounds.Min.X
	loY := int(math.Floor((dirty.Min.Y-top)*float64(pixels))) - bounds.Min.Y
	hiY := int(math.Ceil((dirty.Max.Y-top)*float64(pixels))) - bounds.Min.Y
	const tile = 64
	loX = int(math.Floor(float64(loX)/tile)) * tile
	loY = int(math.Floor(float64(loY)/tile)) * tile
	hiX = int(math.Ceil(float64(hiX)/tile)) * tile
	hiY = int(math.Ceil(float64(hiY)/tile)) * tile
	return image.Rect(loX, loY, hiX, hiY).Intersect(dst.Bounds())
}

func referenceRegion(rect image.Rectangle, pixels int, offset image.Point) image.Rectangle {
	scale := float64(render.RasterPixelsPerUnit) / float64(pixels)
	return image.Rect(int(math.Floor(float64(rect.Min.X-offset.X)*scale))-2, int(math.Floor(float64(rect.Min.Y-offset.Y)*scale))-2, int(math.Ceil(float64(rect.Max.X-offset.X)*scale))+2, int(math.Ceil(float64(rect.Max.Y-offset.Y)*scale))+2)
}

func (g *Scene) replayGridRegion(section *worldSection, id int64, dirty geom.Bounds, grass bool) {
	pixels := section.renderWidth()
	dst := section.foreground
	if grass {
		dst = section.grass
	}
	bounds := image.Rect(0, pixels, pixels, 2*pixels)
	top := terrain.SectionWindowTop(id)
	rect := dirtyRaster(bounds, top, pixels, dirty, dst)
	if rect.Empty() {
		return
	}
	target := dst.SubImage(rect).(*ebiten.Image)
	target.Clear()
	source := section.mesh.Foreground
	reference := referenceRegion(rect, pixels, image.Pt(0, -pixels))
	region := geom.Bounds{Min: V{X: float64(rect.Min.X) / float64(pixels), Y: float64(rect.Min.Y) / float64(pixels)}, Max: V{X: float64(rect.Max.X) / float64(pixels), Y: float64(rect.Max.Y) / float64(pixels)}}
	candidates := render.AttachmentCandidates(source.Growth, source.GrowthIndex, region)
	if !grass {
		for _, m := range render.AttachmentRegionBatches(source.Growth, candidates, reference, false, true) {
			g.drawAttachmentMesh(target, render.ScaleRegionMesh(m, pixels, image.Pt(0, -pixels)))
		}
	}
	if !grass {
		faces := render.ScaleRegionMesh(render.RegionMesh(source.Faces, reference), pixels, image.Pt(0, -pixels))
		g.drawGridFaces(target, faces, top)
		for _, outline := range source.Outlines {
			m := render.ScaleRegionMesh(render.RegionMesh(outline, reference), pixels, image.Pt(0, -pixels))
			m.DrawWithAntialiasing(target, g.world.white, g.antialiasing.GeometryAA())
		}
	} else if len(source.Grass.Indices) > 0 {
		g.drawGridFaces(target, render.ScaleRegionMesh(render.RegionMesh(source.Grass, reference), pixels, image.Pt(0, -pixels)), top)
	}
	if grass || !section.foregroundSplit {
		for _, m := range render.AttachmentRegionBatches(source.Growth, candidates, reference, grass, false) {
			g.drawAttachmentMesh(target, render.ScaleRegionMesh(m, pixels, image.Pt(0, -pixels)))
		}
	}
	g.eraseCuts(target, bounds, top, section.mesh.VisualCuts, pixels)
	if !grass && !section.foregroundSplit {
		g.drawShapeRims(target, section.geometry, pixels, rect)
	}
}

func (g *Scene) replayForegroundVineRegion(dst *ebiten.Image, source []render.TriangleMesh, referenceBounds, nativeBounds image.Rectangle, id int64, pixels int, dirty geom.Bounds, cuts []terrain.RockCut) {
	top := terrain.SectionWindowTop(id)
	rect := dirtyRaster(nativeBounds, top, pixels, dirty, dst)
	if rect.Empty() {
		return
	}
	reference := referenceRegion(rect, pixels, image.Point{})
	scale := float64(pixels) / render.RasterPixelsPerUnit
	// Account for the fractional crop origin used by ScaleVegetationMesh.
	dx := float32(float64(referenceBounds.Min.X)*scale - float64(nativeBounds.Min.X))
	dy := float32(float64(referenceBounds.Min.Y)*scale - float64(nativeBounds.Min.Y))
	patch := ebiten.NewImage(rect.Dx(), rect.Dy())
	for _, m := range source {
		m = render.RegionMesh(m, reference)
		m = render.ScaleRegionMesh(m, pixels, image.Point{})
		m = render.TranslateMesh(m, dx-float32(rect.Min.X), dy-float32(rect.Min.Y))
		m.DrawWithAntialiasing(patch, g.world.white, g.antialiasing.GeometryAA())
	}
	patchBounds := rect.Add(nativeBounds.Min)
	g.eraseCuts(patch, patchBounds, top, cuts, pixels)
	target := dst.SubImage(rect).(*ebiten.Image)
	target.Clear()
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(rect.Min.X), float64(rect.Min.Y))
	target.DrawImage(patch, op)
	patch.Deallocate()
}

func (g *Scene) replayAttachmentRegion(dst *ebiten.Image, source []render.Attachment, index *render.AttachmentSupportIndex, referenceBounds, nativeBounds image.Rectangle, id int64, pixels int, dirty geom.Bounds) {
	top := terrain.SectionWindowTop(id)
	rect := dirtyRaster(nativeBounds, top, pixels, dirty, dst)
	if rect.Empty() {
		return
	}
	target := dst.SubImage(rect).(*ebiten.Image)
	target.Clear()
	scale := float64(pixels) / render.RasterPixelsPerUnit
	dx := float32(float64(referenceBounds.Min.X)*scale - float64(nativeBounds.Min.X))
	dy := float32(float64(referenceBounds.Min.Y)*scale - float64(nativeBounds.Min.Y))
	ref := referenceRegion(rect, pixels, image.Point{})
	region := geom.Bounds{Min: V{X: float64(rect.Min.X+nativeBounds.Min.X) / float64(pixels), Y: float64(rect.Min.Y+nativeBounds.Min.Y)/float64(pixels) + terrain.GenerationMinY}, Max: V{X: float64(rect.Max.X+nativeBounds.Min.X) / float64(pixels), Y: float64(rect.Max.Y+nativeBounds.Min.Y)/float64(pixels) + terrain.GenerationMinY}}
	for _, i := range render.AttachmentCandidates(source, index, region) {
		a := source[i]
		for _, m := range []render.TriangleMesh{a.Mesh, a.Extra} {
			m = render.RegionMesh(m, ref)
			if len(m.Indices) > 0 {
				m = render.ScaleRegionMesh(m, pixels, image.Point{})
				m = render.TranslateMesh(m, dx, dy)
				g.drawAttachmentMesh(target, m)
			}
		}
	}
}

func (g *Scene) maskUploadForeground(u *sectionUpload) {
	bounds := image.Rect(0, u.pixels, u.pixels, 2*u.pixels)
	top := terrain.SectionWindowTop(u.data.ID)
	g.eraseCuts(u.foreground, bounds, top, u.data.VisualCuts, u.pixels)
	g.eraseCuts(u.trim, bounds, top, u.data.VisualCuts, u.pixels)
	if u.foreground != nil && len(u.data.VisualCuts) > 0 {
		if u.trim == nil {
			u.trim = render.NewSectionImageAt(terrain.SectionHeight, u.pixels)
		}
		g.drawShapeRims(u.trim, u.data.Geometry, u.pixels, u.trim.Bounds())
	}
}

// Edits arriving during an upload filter only prepared instances. Rock and
// vine recipes remain valid. A partly drawn attachment layer can restart from
// its cached instances; foreground/background upload progress is retained.
func (g *Scene) carveUpload(cut terrain.RockCut) {
	if u := g.world.upload; u != nil {
		g.carveSectionUpload(u, cut)
	}
	for _, u := range g.world.parked {
		g.carveSectionUpload(u, cut)
	}
}
func (g *Scene) carveSectionUpload(u *sectionUpload, cut terrain.RockCut) {
	top := terrain.SectionTop(u.data.ID)
	if cut.Max.Y <= top+terrain.GenerationMinY || cut.Min.Y >= top+2*SectionHeight {
		return
	}
	geometry, _, _ := cut.Geometry(u.data.ID, u.data.Geometry, g.collisionTolerance)
	geometry, _, removals := cut.PlantsWithChanges(geometry)
	u.data.Geometry = geometry
	edits := editMeshImages(&u.data, cut, removals)
	u.data.AppliedCuts = uint64(len(g.world.cuts))
	grassEdits := edits
	if u.grassData != nil {
		grass := u.grassData
		geometry, _, _ := cut.Geometry(grass.ID, grass.Geometry, g.collisionTolerance)
		geometry, _, changes := cut.PlantsWithChanges(geometry)
		grass.Geometry = geometry
		grassEdits = editMeshImages(grass, cut, changes)
		grass.AppliedCuts = u.data.AppliedCuts
	}
	if !u.initialized {
		return
	}
	u.reconcileGrowthUploads(grassEdits)
	if len(grassEdits.grass) > 0 && u.needed&grassUploadLayer != 0 {
		// Restart only this private product after attachment removal. Its
		// sibling foreground/background cursors retain their progress.
		if u.grass != nil {
			u.grass.Deallocate()
			u.grass = nil
		}
		if u.grassCursor.surface != nil {
			u.grassCursor.surface.Deallocate()
		}
		u.grassCursor = uploadCursor{}
		u.grassInitialized, u.grassFacesDone = false, false
	}
	if len(edits.growth) > 0 {
		u.raster.Foreground.Growth = markRasterDeaths(u.raster.Foreground.Growth, edits.growth)
		if u.foreground != nil && u.stage >= uploadRockUnderlay {
			recipe := render.StructuralSection(u.data)
			if u.stage == uploadRockUnderlay || u.stage == uploadRockGrowth || u.stage == uploadGrass {
				processed := append([]render.Attachment(nil), recipe.Foreground.Growth...)
				for i := range processed {
					if i < u.next {
						continue
					}
					if u.stage == uploadRockUnderlay || (u.stage == uploadRockGrowth && !processed[i].Underlay) || (u.stage == uploadGrass && processed[i].Kind == render.GrassAttachment) {
						processed[i].Dead = true
					}
				}
				recipe.Foreground.Growth = processed
			}
			temporary := &worldSection{foreground: u.foreground, trim: u.trim, foregroundSplit: true, geometry: geometry, mesh: &recipe, pixels: u.pixels}
			if u.stage > uploadRockUnderlay && len(edits.rock) > 0 {
				g.replayGridRegions(temporary, u.data.ID, edits.rock, false)
				if u.trim != nil && u.stage >= uploadRockGrowth {
					trim := render.MaskForeground(recipe.Foreground, render.StructuralTopSpan)
					g.replayForegroundAttachments(u.trim, trim, u.data.ID, u.pixels, edits.rock, false, recipe.VisualCuts, nil)
				}
			} else {
				g.eraseCuts(u.foreground, image.Rect(0, u.pixels, u.pixels, 2*u.pixels), terrain.SectionWindowTop(u.data.ID), []terrain.RockCut{cut}, u.pixels)
			}
		}
	}
	if len(edits.plants)+len(edits.webs) > 0 {
		u.raster.Plants = markRasterDeaths(u.raster.Plants, edits.plants)
		u.raster.Webs = markRasterDeaths(u.raster.Webs, edits.webs)
		processed := func(list []render.Attachment, kind vegetationProduct) []render.Attachment {
			cursor := &u.plants[kind].cursor
			stage := uploadMushrooms
			if kind == websProduct {
				stage = uploadSpiderWebs
			}
			if !u.plantsInitialized {
				return nil
			}
			if cursor.stage < stage {
				return nil
			}
			if cursor.stage > stage {
				return list
			}
			end := min(cursor.next, len(list))
			out := list[:end]
			if cursor.instancePart == 1 && end < len(list) {
				out = append(append([]render.Attachment(nil), out...), list[end])
				out[end].Extra = render.TriangleMesh{}
			}
			return out
		}
		if u.spiderWebs != nil && len(edits.webs) > 0 {
			g.replayAttachmentRegions(u.spiderWebs, processed(u.data.Webs, websProduct), u.data.WebIndex, u.data.SpiderWebsBounds, u.raster.SpiderWebsBounds, u.data.ID, u.pixels, edits.spiderWebs)
		}
		if u.mushrooms != nil && len(edits.plants) > 0 {
			g.replayAttachmentRegions(u.mushrooms, processed(u.data.Plants, mushroomsProduct), u.data.PlantIndex, u.data.MushroomsBounds, u.raster.MushroomsBounds, u.data.ID, u.pixels, edits.mushrooms)
		}
	}
}

func (g *Scene) eraseCuts(dst *ebiten.Image, bounds image.Rectangle, top float64, cuts []terrain.RockCut, pixels int) {
	render.EraseCutsWithAntialiasing(dst, bounds, top, cuts, pixels, g.antialiasing.GeometryAA())
}

// A fallback can outlive the query snapshot that generated it. Its attachment
// IDs and structural loops must be edited against its own physical snapshot,
// rather than the latest query's unrelated placement IDs or cut boundaries.
func (g *Scene) editLayerSource(source render.SectionMesh, kind render.MeshLayers, cut terrain.RockCut, current terrain.DecorationChanges) (render.SectionMesh, meshImageEdits) {
	geometry := source.Geometry
	if kind == render.ForegroundLayer {
		geometry, _, _ = cut.Geometry(source.ID, geometry, g.collisionTolerance)
	}
	if kind == render.MushroomsLayer || kind == render.WebsLayer {
		var own terrain.DecorationChanges
		geometry, _, own = cut.PlantsWithChanges(geometry)
		current.Mushrooms = append(append([][2]int64(nil), current.Mushrooms...), own.Mushrooms...)
		current.Webs = append(append([][2]int64(nil), current.Webs...), own.Webs...)
	}
	edits := editMeshImages(&source, cut, current)
	source.Geometry, source.AppliedCuts = geometry, uint64(len(g.world.cuts))
	return source, edits
}

// Union dirty tiles and join adjacent runs into rectangles. Large blasts then
// replay each mesh and cut mask once per rectangle rather than once per tile,
// while gaps between unrelated attachments stay untouched.
func dirtyTiles(dst *ebiten.Image, bounds image.Rectangle, top float64, pixels int, regions []geom.Bounds) []geom.Bounds {
	if dst == nil {
		return nil
	}
	const tile = 64
	seen := make(map[image.Point]bool)
	var tiles []image.Point
	for _, region := range regions {
		r := dirtyRaster(bounds, top, pixels, region, dst)
		for y := r.Min.Y; y < r.Max.Y; y += tile {
			for x := r.Min.X; x < r.Max.X; x += tile {
				p := image.Pt(x, y)
				if !seen[p] {
					seen[p] = true
					tiles = append(tiles, p)
				}
			}
		}
	}
	slices.SortFunc(tiles, func(a, b image.Point) int {
		if a.Y != b.Y {
			return a.Y - b.Y
		}
		return a.X - b.X
	})
	var rects []image.Rectangle
	previous := make(map[[2]int]int)
	for i := 0; i < len(tiles); {
		y := tiles[i].Y
		current := make(map[[2]int]int)
		for i < len(tiles) && tiles[i].Y == y {
			x, end := tiles[i].X, tiles[i].X+tile
			i++
			for i < len(tiles) && tiles[i].Y == y && tiles[i].X == end {
				end += tile
				i++
			}
			key := [2]int{x, end}
			index, ok := previous[key]
			if ok && rects[index].Max.Y == y {
				rects[index].Max.Y = y + tile
			} else {
				index = len(rects)
				rects = append(rects, image.Rect(x, y, end, y+tile))
			}
			current[key] = index
		}
		previous = current
	}
	result := make([]geom.Bounds, 0, len(rects))
	for _, rect := range rects {
		r := rect.Intersect(dst.Bounds()).Add(bounds.Min)
		// Interior pixel coordinates avoid floating-point roundoff snapping a
		// nominal tile edge into its neighboring tile in dirtyRaster.
		result = append(result, geom.Bounds{
			Min: V{X: (float64(r.Min.X) + .25) / float64(pixels), Y: top + (float64(r.Min.Y)+.25)/float64(pixels)},
			Max: V{X: (float64(r.Max.X) - .25) / float64(pixels), Y: top + (float64(r.Max.Y)-.25)/float64(pixels)},
		})
	}
	return result
}

func (g *Scene) replayGridRegions(section *worldSection, id int64, regions []geom.Bounds, grass bool) {
	pixels := section.renderWidth()
	dst := section.foreground
	if grass {
		dst = section.grass
	}
	for _, dirty := range dirtyTiles(dst, image.Rect(0, pixels, pixels, 2*pixels), terrain.SectionWindowTop(id), pixels, regions) {
		g.replayGridRegion(section, id, dirty, grass)
	}
}

func (g *Scene) replayAttachmentRegions(dst *ebiten.Image, source []render.Attachment, index *render.AttachmentSupportIndex, referenceBounds, nativeBounds image.Rectangle, id int64, pixels int, regions []geom.Bounds) {
	for _, dirty := range dirtyTiles(dst, nativeBounds, terrain.SectionWindowTop(id), pixels, regions) {
		g.replayAttachmentRegion(dst, source, index, referenceBounds, nativeBounds, id, pixels, dirty)
	}
}

// Raster geometry and atlas coordinates do not change when an instance dies.
func markRasterDeaths(source []render.Attachment, removed []int) []render.Attachment {
	if len(source) == 0 || len(removed) == 0 {
		return source
	}
	out := append([]render.Attachment(nil), source...)
	for _, i := range removed {
		out[i].Dead = true
	}
	return out
}

// Replay only the published attachment span at its own width. Structural trim
// also carries fresh cut rims, after every growth layer in composition order.
func (g *Scene) replayForegroundAttachments(dst *ebiten.Image, source render.GridMesh, id int64, pixels int, regions []geom.Bounds, underlay bool, cuts []terrain.RockCut, rims *terrain.Geometry) {
	bounds := image.Rect(0, pixels, pixels, 2*pixels)
	top := terrain.SectionWindowTop(id)
	for _, dirty := range dirtyTiles(dst, bounds, top, pixels, regions) {
		rect := dirtyRaster(bounds, top, pixels, dirty, dst)
		if rect.Empty() {
			continue
		}
		target := dst.SubImage(rect).(*ebiten.Image)
		target.Clear()
		reference := referenceRegion(rect, pixels, image.Pt(0, -pixels))
		region := geom.Bounds{Min: V{X: float64(rect.Min.X) / float64(pixels), Y: float64(rect.Min.Y) / float64(pixels)}, Max: V{X: float64(rect.Max.X) / float64(pixels), Y: float64(rect.Max.Y) / float64(pixels)}}
		candidates := render.AttachmentCandidates(source.Growth, source.GrowthIndex, region)
		for _, m := range render.AttachmentRegionBatches(source.Growth, candidates, reference, false, underlay) {
			g.drawAttachmentMesh(target, render.ScaleRegionMesh(m, pixels, image.Pt(0, -pixels)))
		}
		g.eraseCuts(target, bounds, top, cuts, pixels)
		if rims != nil {
			g.drawShapeRims(target, rims, pixels, rect)
		}
	}
}

func (g *Scene) editGrowthImages(s *worldSection, id int64, cut terrain.RockCut, geometry *terrain.Geometry, removals terrain.DecorationChanges, dirty geom.Bounds, overlap bool) {
	for kind := growthProduct(0); kind < growthProducts; kind++ {
		p := &s.growth[kind]
		if p.source == nil {
			continue
		}
		source := *p.source
		oldCuts := source.VisualCuts
		edits := editMeshImages(&source, cut, removals)
		source.Geometry, source.AppliedCuts = geometry, uint64(len(g.world.cuts))
		p.source = &source
		g.captureGrowthBurn(s, kind, id, cut, oldCuts, edits)
		if p.image == nil {
			continue
		}
		bounds := image.Rect(0, p.pixels, p.pixels, 2*p.pixels)
		if overlap || len(edits.rock) > 0 {
			g.replayForegroundAttachments(p.image, source.Foreground, id, p.pixels, append(edits.rock, dirty), kind == underGrowth, source.VisualCuts, nil)
		} else {
			g.eraseCuts(p.image, bounds, terrain.SectionWindowTop(id), []terrain.RockCut{cut}, p.pixels)
		}
	}
}

func (g *Scene) captureGrowthBurn(s *worldSection, kind growthProduct, id int64, cut terrain.RockCut, oldCuts []terrain.RockCut, edits meshImageEdits) {
	p := &s.growth[kind]
	if g.decorationBurn == nil || p.image == nil || s.foreground == nil {
		return
	}
	source := p.source.Foreground
	local := geom.Bounds{Min: cut.Min.Sub(V{Y: terrain.SectionTop(id)}), Max: cut.Max.Sub(V{Y: terrain.SectionTop(id)})}
	var whole, partial []render.Attachment
	for _, i := range edits.growth {
		whole = append(whole, source.Growth[i])
	}
	for _, i := range render.AttachmentCandidates(source.Growth, source.GrowthIndex, local) {
		partial = append(partial, source.Growth[i])
	}
	bounds := image.Rect(0, p.pixels, p.pixels, 2*p.pixels)
	progress := p.reveal.Progress() * s.foregroundReveal.Progress()
	g.burnAttachments(p.image, whole, image.Rectangle{}, bounds, id, p.pixels, false, progress, oldCuts, nil)
	g.burnAttachments(p.image, partial, image.Rectangle{}, bounds, id, p.pixels, false, progress, oldCuts, &cut)
}

func (u *sectionUpload) reconcileGrowthUploads(edits meshImageEdits) {
	data := u.detailData()
	for kind := growthProduct(0); kind < growthProducts; kind++ {
		if u.needed&kind.layer() == 0 {
			continue
		}
		for _, i := range edits.growth {
			if kind.span().Contains(data.Foreground.Growth[i]) {
				u.growth[kind].deallocate()
				u.growth[kind] = growthUpload{}
				break
			}
		}
	}
}

func (g *Scene) drawShapeRims(dst *ebiten.Image, geometry *terrain.Geometry, pixels int, rect image.Rectangle) {
	if geometry == nil || len(geometry.Cuts) == 0 {
		return
	}
	bounds := geom.Bounds{Min: V{X: float64(rect.Min.X)/float64(pixels) - .01, Y: float64(rect.Min.Y)/float64(pixels) - .01}, Max: V{X: float64(rect.Max.X)/float64(pixels) + .01, Y: float64(rect.Max.Y)/float64(pixels) + .01}}
	m := render.PrepareShapeRimsInBounds(geometry, g.view, bounds)
	m = render.RegionMesh(m, referenceRegion(rect, pixels, image.Pt(0, -pixels)))
	if len(m.Indices) == 0 {
		return
	}
	m = render.ScaleRegionMesh(m, pixels, image.Pt(0, -pixels))
	g.drawGridFaces(dst, m, terrain.SectionWindowTop(-geometry.Collision.ID))
}
