package infinicave

import (
	"image"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

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
	bounds := image.Rect(0, u.pixels, u.pixels, 2*u.pixels)
	g.eraseCuts(p.image, bounds, terrain.SectionWindowTop(data.ID), data.VisualCuts, u.pixels)
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
