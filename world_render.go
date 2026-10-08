package infinicave

import (
	"image"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func (w *world) renderWidth() int {
	if w.pixels > 0 {
		return w.pixels
	}
	return render.RasterPixelsPerUnit
}

func (s *worldSection) renderWidth() int {
	if s.pixels > 0 {
		return s.pixels
	}
	return render.RasterPixelsPerUnit
}

func (g *Scene) drawGridFaces(dst *ebiten.Image, mesh render.TriangleMesh, top float64) {
	g.drawGridFacesWithAA(dst, mesh, top, true)
}

func (g *Scene) drawGridFacesWithAA(dst *ebiten.Image, mesh render.TriangleMesh, top float64, aa bool) {
	if len(mesh.Indices) == 0 {
		return
	}
	light := terrain.RockLightDirection(g.orientation)
	dst.DrawTrianglesShader32(mesh.Vertices, mesh.Indices, g.material, &ebiten.DrawTrianglesShaderOptions{
		AntiAlias: aa,
		Uniforms: map[string]any{
			"Texture": float32(g.texture),
			"Offset":  []float32{317, float32(top*render.RasterPixelsPerUnit) + 791},
			"Light":   []float32{float32(light.X), float32(light.Y), float32(light.Z)},
		},
	})
}

func (w *world) draw(dst *ebiten.Image, y, height float64, fog *render.FogRenderer, background *render.BackgroundRenderer) {
	view := render.TargetTransform(dst)
	y = render.RenderAlignedY(y, view.Pixels)
	if background != nil {
		background.Draw(dst, w, y, height, view)
	} else {
		w.DrawBackground(dst, y, height, view)
	}
	low, high := visibleSections(y, height)
	if fog != nil {
		for id := low; id <= high; id++ {
			fog.DrawSection(dst, terrain.SectionTop(id), y, view)
		}
	}
	for id := max(0, low-1); id <= high+1; id++ {
		if section := w.sections[id]; section != nil {
			w.drawSupportedVegetation(dst, section.mushrooms, section.mushroomsBounds, terrain.SectionWindowTop(id), y, height, section.vegetationWidth(), view, false)
		}
	}
	w.DrawForegroundRocks(dst, y, height, 0, view)
	// Keep complete foreground stems in their owner's padded window.
	for id := max(0, low-1); id <= high+1; id++ {
		if section := w.sections[id]; section != nil {
			w.drawSupportedVegetation(dst, section.foregroundVines, section.foregroundVinesBounds, terrain.SectionWindowTop(id), y, height, section.vegetationWidth(), view, false)
		}
	}
}

// DrawBackground supplies cached background rock and vines to the effect pass.
func (w *world) DrawBackground(dst *ebiten.Image, y, height float64, view render.RenderTransform) {
	low, high := visibleSections(y, height)
	for id := low; id <= high; id++ {
		if section := w.sections[id]; section != nil && section.terrain != nil {
			op := &ebiten.DrawImageOptions{}
			pixels := section.backgroundWidth()
			scale := float64(view.Pixels) / float64(pixels)
			op.GeoM.Translate(float64(render.BackgroundRasterBounds(pixels).Min.X), (terrain.SectionTop(id)-y)*float64(pixels))
			op.GeoM.Scale(scale, scale)
			op.GeoM.Translate(view.OffsetX, 0)
			dst.DrawImage(section.terrain, op)
		}
	}
	// A vine is drawn once in world coordinates, even while crossing a seam.
	for id := max(0, low-1); id <= high+1; id++ {
		if section := w.sections[id]; section != nil {
			w.drawSupportedVegetation(dst, section.vines, section.vinesBounds, terrain.SectionWindowTop(id), y, height, section.vegetationWidth(), view, true)
		}
	}
}

// The same current images supply both visible rock and the shadow silhouette.
// Runtime cuts redraw these images immediately, so no shadow cache goes stale.
func (w *world) DrawForegroundRocks(dst *ebiten.Image, y, height, offsetX float64, view render.RenderTransform) {
	low, high := visibleSections(y, height)
	for id := low; id <= high; id++ {
		if section := w.sections[id]; section != nil && section.foreground != nil {
			op := &ebiten.DrawImageOptions{}
			pixels := section.renderWidth()
			scale := float64(view.Pixels) / float64(pixels)
			op.GeoM.Translate(offsetX*float64(pixels), (terrain.SectionTop(id)-y)*float64(pixels))
			op.GeoM.Scale(scale, scale)
			op.GeoM.Translate(view.OffsetX, 0)
			dst.DrawImage(section.foreground, op)
		}
	}
}

func (s *worldSection) backgroundWidth() int {
	if s.backgroundPixels > 0 {
		return s.backgroundPixels
	}
	return s.renderWidth()
}
func (s *worldSection) vegetationWidth() int {
	if s.vegetationPixels > 0 {
		return s.vegetationPixels
	}
	return s.renderWidth()
}
func (s *worldSection) needsRefresh(pixels int) bool {
	if s.mesh != nil {
		mask := s.mesh.LayerMask()
		if mask&render.ForegroundLayer != 0 && s.foreground == nil {
			return true
		}
		if mask&render.BackgroundLayer != 0 && s.terrain == nil {
			return true
		}
		if mask&render.VegetationLayer != 0 && s.vegetationPending {
			return true
		}
	}
	return (s.foreground != nil && s.renderWidth() != pixels) || (s.terrain != nil && s.backgroundWidth() != pixels) || (!s.vegetationPending && s.vegetationWidth() != pixels)
}

// Cross-band plants are visible only where their supporting rock layer exists.
// Adjacent clips partition the destination, preserving each stem's world origin.
func (w *world) drawSupportedVegetation(dst, layer *ebiten.Image, bounds image.Rectangle, top, y, height float64, pixels int, view render.RenderTransform, background bool) {
	if layer == nil {
		return
	}
	low, high := visibleSections(y, height)
	for id := low; id <= high; id++ {
		section := w.sections[id]
		if section == nil || (background && section.terrain == nil) || (!background && section.foreground == nil) {
			continue
		}
		minY := int(math.Round((terrain.SectionTop(id) - y) * float64(view.Pixels)))
		maxY := minY + view.Pixels
		clip := image.Rect(dst.Bounds().Min.X, minY, dst.Bounds().Max.X, maxY).Intersect(dst.Bounds())
		if clip.Empty() {
			continue
		}
		render.DrawVegetation(dst.SubImage(clip).(*ebiten.Image), layer, bounds, top, y, pixels, view)
	}
}

func (w *world) BackgroundVisible(y, height float64) bool {
	lo, hi := visibleSections(y, height)
	for id := lo; id <= hi; id++ {
		if section := w.sections[id]; section != nil && section.terrain != nil {
			return true
		}
	}
	return false
}
