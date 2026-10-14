package infinicave

import (
	"image"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

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
func (g *Scene) editSectionImages(section *worldSection, id int64, cut terrain.RockCut, removals terrain.DecorationChanges) {
	if section.mesh == nil || cut.Max.Y <= terrain.SectionTop(id)+terrain.GenerationMinY || cut.Min.Y >= terrain.SectionTop(id)+2*SectionHeight {
		return
	}
	mesh := *section.mesh
	oldCuts := mesh.VisualCuts
	edits := editMeshImages(&mesh, cut, removals)
	mesh.Geometry = section.geometry
	mesh.AppliedCuts = uint64(len(g.world.cuts))
	section.mesh = &mesh
	if section.foreground == nil {
		return
	}
	if g.world.white == nil {
		g.world.white = ebiten.NewImage(1, 1)
		g.world.white.Fill(color.White)
	}
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
	if overlap || len(edits.rock) > 0 {
		g.replayGridRegions(section, id, append(edits.rock, dirty), false)
	} else {
		// The common isolated cut operates directly on the finished image.
		g.eraseCuts(section.foreground, bounds, top, []terrain.RockCut{cut}, pixels)
		rect := dirtyRaster(bounds, top, pixels, dirty, section.foreground)
		if !rect.Empty() {
			g.drawShapeRims(section.foreground.SubImage(rect).(*ebiten.Image), section.geometry, pixels, rect)
		}
	}
	if section.grass != nil {
		if len(edits.grass) > 0 || overlap {
			g.replayGridRegions(section, id, append(edits.grass, dirty), true)
		} else {
			g.eraseCuts(section.grass, bounds, top, []terrain.RockCut{cut}, pixels)
		}
	}
	if section.vegetationPending {
		return
	}
	if overlap {
		g.replayForegroundVineRegion(section.foregroundVines, mesh.ForegroundVines, mesh.ForegroundVinesBounds, section.foregroundVinesBounds, id, section.vegetationWidth(), dirty, mesh.VisualCuts)
	} else {
		g.eraseCuts(section.foregroundVines, section.foregroundVinesBounds, top, []terrain.RockCut{cut}, section.vegetationWidth())
	}
	if len(edits.plants) > 0 {
		g.replayAttachmentRegions(section.mushrooms, mesh.Plants, mesh.PlantIndex, mesh.MushroomsBounds, section.mushroomsBounds, id, section.vegetationWidth(), edits.mushrooms)
	}
	if len(edits.webs) > 0 {
		g.replayAttachmentRegions(section.spiderWebs, mesh.Webs, mesh.WebIndex, mesh.SpiderWebsBounds, section.spiderWebsBounds, id, section.vegetationWidth(), edits.spiderWebs)
	}
	if len(edits.grass) > 0 && len(mesh.Foreground.Grass.Indices) == 0 && !render.HasLiveAttachments(mesh.Foreground.Growth, true) && section.grass != nil {
		section.grass.Deallocate()
		section.grass = nil
	}
	if mesh.BakedInstances {
		if len(edits.plants) > 0 && !render.HasLiveAttachments(mesh.Plants, false) && section.mushrooms != nil {
			section.mushrooms.Deallocate()
			section.mushrooms = nil
		}
		if len(edits.webs) > 0 && !render.HasLiveAttachments(mesh.Webs, false) && section.spiderWebs != nil {
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
	for _, m := range render.AttachmentRegionBatches(source.Growth, candidates, reference, grass, false) {
		g.drawAttachmentMesh(target, render.ScaleRegionMesh(m, pixels, image.Pt(0, -pixels)))
	}
	g.eraseCuts(target, bounds, top, section.mesh.VisualCuts, pixels)
	if !grass {
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
	g.eraseCuts(u.grass, bounds, top, u.data.VisualCuts, u.pixels)
	if u.foreground != nil {
		g.drawShapeRims(u.foreground, u.data.Geometry, u.pixels, u.foreground.Bounds())
	}
}
func (g *Scene) maskUploadPlants(section *worldSection, u *sectionUpload) {
	top := terrain.SectionWindowTop(u.data.ID)
	g.eraseCuts(section.foregroundVines, section.foregroundVinesBounds, top, u.data.VisualCuts, u.pixels)
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
	if !u.initialized {
		return
	}
	if len(edits.growth) > 0 {
		u.raster.Foreground.Growth = markRasterDeaths(u.raster.Foreground.Growth, edits.growth)
		if u.foreground != nil && u.stage >= uploadRockUnderlay {
			recipe := u.data
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
			temporary := &worldSection{foreground: u.foreground, grass: u.grass, geometry: geometry, mesh: &recipe, pixels: u.pixels}
			if u.stage > uploadRockUnderlay && len(edits.rock) > 0 {
				g.replayGridRegions(temporary, u.data.ID, edits.rock, false)
			} else {
				g.eraseCuts(u.foreground, image.Rect(0, u.pixels, u.pixels, 2*u.pixels), terrain.SectionWindowTop(u.data.ID), []terrain.RockCut{cut}, u.pixels)
			}
			if u.grass != nil && len(edits.grass) > 0 {
				g.replayGridRegions(temporary, u.data.ID, edits.grass, true)
			}
		}
	}
	if len(edits.plants)+len(edits.webs) > 0 {
		u.raster.Plants = markRasterDeaths(u.raster.Plants, edits.plants)
		u.raster.Webs = markRasterDeaths(u.raster.Webs, edits.webs)
		processed := func(list []render.Attachment, stage int) []render.Attachment {
			if u.stage < stage {
				return nil
			}
			if u.stage > stage {
				return list
			}
			end := min(u.next, len(list))
			out := list[:end]
			if u.instancePart == 1 && end < len(list) {
				out = append(append([]render.Attachment(nil), out...), list[end])
				out[end].Extra = render.TriangleMesh{}
			}
			return out
		}
		if u.spiderWebs != nil && len(edits.webs) > 0 {
			g.replayAttachmentRegions(u.spiderWebs, processed(u.data.Webs, uploadSpiderWebs), u.data.WebIndex, u.data.SpiderWebsBounds, u.raster.SpiderWebsBounds, u.data.ID, u.pixels, edits.spiderWebs)
		}
		if u.mushrooms != nil && len(edits.plants) > 0 {
			g.replayAttachmentRegions(u.mushrooms, processed(u.data.Plants, uploadMushrooms), u.data.PlantIndex, u.data.MushroomsBounds, u.raster.MushroomsBounds, u.data.ID, u.pixels, edits.mushrooms)
		}
	}
}

func (g *Scene) eraseCuts(dst *ebiten.Image, bounds image.Rectangle, top float64, cuts []terrain.RockCut, pixels int) {
	render.EraseCutsWithAntialiasing(dst, bounds, top, cuts, pixels, g.antialiasing.GeometryAA())
}
