package infinicave

import (
	"image"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

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
