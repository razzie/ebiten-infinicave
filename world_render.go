package infinicave

import (
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
	if len(mesh.Indices) == 0 {
		return
	}
	light := terrain.RockLightDirection(g.orientation)
	dst.DrawTrianglesShader32(mesh.Vertices, mesh.Indices, g.material, &ebiten.DrawTrianglesShaderOptions{
		AntiAlias: true,
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
			if section := w.sections[id]; section != nil && section.terrain != nil {
				fog.DrawSection(dst, terrain.SectionTop(id), y, view)
			}
		}
	}
	for id := max(0, low-1); id <= high+1; id++ {
		if section := w.sections[id]; section != nil {
			render.DrawVegetation(dst, section.mushrooms, section.mushroomsBounds, terrain.SectionWindowTop(id), y, section.renderWidth(), view)
		}
	}
	w.DrawForegroundRocks(dst, y, height, 0, view)
	// Keep complete foreground stems in their owner's padded window.
	for id := max(0, low-1); id <= high+1; id++ {
		if section := w.sections[id]; section != nil {
			render.DrawVegetation(dst, section.foregroundVines, section.foregroundVinesBounds, terrain.SectionWindowTop(id), y, section.renderWidth(), view)
		}
	}
}

// DrawBackground supplies cached background rock and vines to the effect pass.
func (w *world) DrawBackground(dst *ebiten.Image, y, height float64, view render.RenderTransform) {
	low, high := visibleSections(y, height)
	for id := low; id <= high; id++ {
		if section := w.sections[id]; section != nil {
			op := &ebiten.DrawImageOptions{}
			pixels := section.renderWidth()
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
			render.DrawVegetation(dst, section.vines, section.vinesBounds, terrain.SectionWindowTop(id), y, section.renderWidth(), view)
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
