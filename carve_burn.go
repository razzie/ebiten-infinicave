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

// Capture only decorations already published on screen. Stored cuts and
// unfinished uploads never create remnants when their layers later appear.
func (g *Scene) captureDecorationBurns(s *worldSection, id int64, cut terrain.RockCut, oldCuts []terrain.RockCut, edits meshImageEdits) {
	if g.decorationBurn == nil {
		return
	}
	mesh := s.mesh
	pixels := s.renderWidth()
	bounds := image.Rect(0, pixels, pixels, 2*pixels)
	top := terrain.SectionTop(id)
	localCut := geom.Bounds{Min: cut.Min.Sub(V{Y: top}), Max: cut.Max.Sub(V{Y: top})}
	candidates := render.AttachmentCandidates(mesh.Foreground.Growth, mesh.Foreground.GrowthIndex, localCut)
	for _, grass := range []bool{false, true} {
		dst := s.foreground
		if grass {
			dst = s.grass
		}
		if dst == nil {
			continue
		}
		var whole, partial []render.Attachment
		for _, i := range edits.growth {
			a := mesh.Foreground.Growth[i]
			if a.Kind != render.EdgeAttachment && (a.Kind == render.GrassAttachment) == grass {
				whole = append(whole, a)
			}
		}
		for _, i := range candidates {
			a := mesh.Foreground.Growth[i]
			if a.Kind != render.EdgeAttachment && !a.PreservesCutPixels() && (a.Kind == render.GrassAttachment) == grass {
				partial = append(partial, a)
			}
		}
		progress := s.foregroundReveal.Progress()
		if grass {
			progress = min(progress, s.grassReveal.Progress())
		}
		g.burnAttachments(dst, whole, image.Rectangle{}, bounds, id, pixels, grass, progress, nil, nil)
		g.burnAttachments(dst, partial, image.Rectangle{}, bounds, id, pixels, grass, progress, oldCuts, &cut)
	}
	selected := func(source []render.Attachment, indices []int) []render.Attachment {
		out := make([]render.Attachment, 0, len(indices))
		for _, i := range indices {
			out = append(out, source[i])
		}
		return out
	}
	g.burnAttachments(s.mushrooms, selected(mesh.Plants, edits.plants), mesh.MushroomsBounds, s.mushroomsBounds, id, s.plantWidth(mushroomsProduct), false, s.plantReveal(mushroomsProduct).Progress(), nil, nil)
	g.burnAttachments(s.spiderWebs, selected(mesh.Webs, edits.webs), mesh.SpiderWebsBounds, s.spiderWebsBounds, id, s.plantWidth(websProduct), false, s.plantReveal(websProduct).Progress(), nil, nil)
	if s.foregroundVines != nil {
		pixels := s.plantWidth(foregroundVinesProduct)
		bounds := s.foregroundVinesBounds
		rect := burnRaster(geom.Bounds{Min: cut.Min, Max: cut.Max}, bounds, terrain.SectionWindowTop(id), pixels).Intersect(s.foregroundVines.Bounds())
		if !rect.Empty() {
			patch := ebiten.NewImage(rect.Dx(), rect.Dy())
			patch.DrawImage(s.foregroundVines.SubImage(rect).(*ebiten.Image), nil)
			g.keepBurnCut(patch, rect.Add(bounds.Min), id, pixels, cut)
			g.addBurnPatch(patch, rect.Add(bounds.Min), id, pixels, false, s.plantReveal(foregroundVinesProduct).Progress())
		}
	}
}

func burnRaster(region geom.Bounds, bounds image.Rectangle, top float64, pixels int) image.Rectangle {
	p := float64(pixels)
	return image.Rect(int(math.Floor(region.Min.X*p))-1, int(math.Floor((region.Min.Y-top)*p))-1,
		int(math.Ceil(region.Max.X*p))+1, int(math.Ceil((region.Max.Y-top)*p))+1).Sub(bounds.Min)
}

func (g *Scene) burnAttachments(dst *ebiten.Image, source []render.Attachment, referenceBounds, nativeBounds image.Rectangle, id int64, pixels int, grass bool, progress float64, oldCuts []terrain.RockCut, cut *terrain.RockCut) {
	if dst == nil || len(source) == 0 {
		return
	}
	top := terrain.SectionTop(id)
	var regions []geom.Bounds
	for _, a := range source {
		region := geom.Bounds{Min: a.Min.Add(V{Y: top}), Max: a.Max.Add(V{Y: top})}
		if cut != nil {
			region.Min.X, region.Min.Y = max(region.Min.X, cut.Min.X), max(region.Min.Y, cut.Min.Y)
			region.Max.X, region.Max.Y = min(region.Max.X, cut.Max.X), min(region.Max.Y, cut.Max.Y)
		}
		regions = append(regions, region)
	}
	windowTop := terrain.SectionWindowTop(id)
	for _, region := range dirtyTiles(dst, nativeBounds, windowTop, pixels, regions) {
		rect := dirtyRaster(nativeBounds, windowTop, pixels, region, dst)
		patch := ebiten.NewImage(rect.Dx(), rect.Dy())
		scale := float64(pixels) / render.RasterPixelsPerUnit
		dx := float32(float64(referenceBounds.Min.X)*scale - float64(nativeBounds.Min.X+rect.Min.X))
		dy := float32(float64(referenceBounds.Min.Y)*scale - float64(nativeBounds.Min.Y+rect.Min.Y))
		ref := image.Rect(int(math.Floor(-float64(dx)/scale))-2, int(math.Floor(-float64(dy)/scale))-2,
			int(math.Ceil((float64(rect.Dx())-float64(dx))/scale))+2, int(math.Ceil((float64(rect.Dy())-float64(dy))/scale))+2)
		for _, a := range source {
			for _, m := range []render.TriangleMesh{a.Mesh, a.Extra} {
				m = render.RegionMesh(m, ref)
				m = render.ScaleRegionMesh(m, pixels, image.Point{})
				g.drawAttachmentMesh(patch, render.TranslateMesh(m, dx, dy))
			}
		}
		bounds := rect.Add(nativeBounds.Min)
		g.eraseCuts(patch, bounds, windowTop, oldCuts, pixels)
		if cut != nil {
			g.keepBurnCut(patch, bounds, id, pixels, *cut)
		}
		g.addBurnPatch(patch, bounds, id, pixels, grass, progress)
	}
}

func (g *Scene) keepBurnCut(patch *ebiten.Image, bounds image.Rectangle, id int64, pixels int, cut terrain.RockCut) {
	// Erasing the complement keeps precisely the newly cut decoration pixels.
	mask := ebiten.NewImage(bounds.Dx(), bounds.Dy())
	mask.Fill(color.White)
	g.eraseCuts(mask, bounds, terrain.SectionWindowTop(id), []terrain.RockCut{cut}, pixels)
	patch.DrawImage(mask, &ebiten.DrawImageOptions{Blend: ebiten.BlendDestinationOut})
	mask.Deallocate()
}

func (g *Scene) addBurnPatch(patch *ebiten.Image, bounds image.Rectangle, id int64, pixels int, grass bool, progress float64) {
	origin := V{X: float64(bounds.Min.X) / float64(pixels), Y: terrain.SectionWindowTop(id) + float64(bounds.Min.Y)/float64(pixels)}
	// Capture only pixels already revealed, including plants in neighboring
	// support bands. Carving must not flash decorations that are still hidden.
	if g.appearanceFade != nil {
		low, high := visibleSections(origin.Y, float64(bounds.Dy())/float64(pixels))
		complete := progress >= 1
		for band := low; band <= high; band++ {
			if s := g.world.sections[band]; s != nil && s.foregroundReveal.Progress() < 1 {
				complete = false
			}
		}
		if !complete {
			visible := ebiten.NewImage(bounds.Dx(), bounds.Dy())
			for band := low; band <= high; band++ {
				s := g.world.sections[band]
				if s == nil || s.foreground == nil {
					continue
				}
				minY := int(math.Round((terrain.SectionTop(band) - origin.Y) * float64(pixels)))
				clip := image.Rect(0, minY, bounds.Dx(), minY+pixels).Intersect(visible.Bounds())
				if !clip.Empty() {
					g.appearanceFade.DrawReveal(visible.SubImage(clip).(*ebiten.Image), patch, nil,
						min(progress, s.foregroundReveal.Progress()), ebiten.GeoM{})
				}
			}
			patch.Deallocate()
			patch = visible
		}
	}
	g.decorationBurn.Add(patch, origin, pixels, grass)
}
