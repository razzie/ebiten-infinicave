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
	g.drawGridFacesWithAA(dst, mesh, top, g.antialiasing.GeometryAA())
}

func (g *Scene) drawGridFacesWithAA(dst *ebiten.Image, mesh render.TriangleMesh, top float64, aa bool) {
	if len(mesh.Indices) == 0 {
		return
	}
	if aa {
		// Ebitengine's legacy AntiAlias option rescales destination positions
		// internally without rescaling custom shader uniforms. Use an explicit
		// accumulator so canonical material samples also survive cropped rims.
		size, origin := dst.Bounds().Size(), dst.Bounds().Min
		surface := ebiten.NewImage(size.X*2, size.Y*2)
		local := render.TranslateMesh(mesh, -float32(origin.X), -float32(origin.Y))
		local = render.SupersampledBatch(local, 0, len(local.Indices))
		g.drawGridFacesWithAA(surface, local, top, false)
		op := &ebiten.DrawImageOptions{Filter: ebiten.FilterLinear}
		op.GeoM.Scale(.5, .5)
		op.GeoM.Translate(float64(origin.X), float64(origin.Y))
		dst.DrawImage(surface, op)
		surface.Deallocate()
		return
	}
	light := terrain.RockLightDirection(g.orientation)
	frame := mesh.MaterialFrame
	var raster [4]float32
	if frame.Pixels != 0 {
		origin := dst.Bounds().Min
		raster = [4]float32{float32(frame.OriginX + float64(origin.X)/frame.Samples), float32(frame.OriginY + float64(origin.Y)/frame.Samples), float32(frame.Pixels), float32(frame.Samples)}
	}
	dst.DrawTrianglesShader32(mesh.Vertices, mesh.Indices, g.material, &ebiten.DrawTrianglesShaderOptions{
		AntiAlias: aa,
		Uniforms: map[string]any{
			"Raster":  raster[:],
			"Texture": float32(g.texture),
			"Offset":  []float32{317, float32(top*render.RasterPixelsPerUnit) + 791},
			"Light":   []float32{float32(light.X), float32(light.Y), float32(light.Z)},
		},
	})
}

func (w *world) draw(dst *ebiten.Image, y, height float64, fog *render.FogRenderer, background *render.BackgroundRenderer, passes RenderPasses, context RenderContext, burns *render.BurnDissolve) {
	view := render.TargetTransform(dst)
	y = render.RenderAlignedY(y, view.Pixels)
	if !w.disableBackground && background != nil {
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
	if passes.BehindRocks != nil {
		passes.BehindRocks(context)
	}
	w.DrawForegroundRocks(dst, y, height, 0, view)
	for id := max(0, low-1); id <= high+1; id++ {
		if section := w.sections[id]; section != nil {
			w.drawSupportedVegetation(dst, section.mushrooms, section.mushroomsBounds, terrain.SectionWindowTop(id), y, height, section.plantWidth(mushroomsProduct), view, false, section.plantReveal(mushroomsProduct))
		}
	}
	for id := low; id <= high; id++ {
		if section := w.sections[id]; section != nil {
			w.drawSupportedVegetation(dst, section.spiderWebs, section.spiderWebsBounds, terrain.SectionWindowTop(id), y, height, section.plantWidth(websProduct), view, false, section.plantReveal(websProduct))
		}
	}
	// Keep complete foreground stems in their owner's padded window.
	for id := max(0, low-1); id <= high+1; id++ {
		if section := w.sections[id]; section != nil {
			w.drawSupportedVegetation(dst, section.foregroundVines, section.foregroundVinesBounds, terrain.SectionWindowTop(id), y, height, section.plantWidth(foregroundVinesProduct), view, false, section.plantReveal(foregroundVinesProduct))
		}
	}
	if burns != nil {
		burns.Draw(dst, y, view, false)
	}
	if passes.AboveRocks != nil {
		passes.AboveRocks(context)
	}
	w.drawGrass(dst, y, height, 0, view)
	if burns != nil {
		burns.Draw(dst, y, view, true)
	}
}

// DrawBackground supplies cached background rock and vines to the effect pass.
func (w *world) DrawBackground(dst *ebiten.Image, y, height float64, view render.RenderTransform) {
	if w.disableBackground {
		return
	}
	low, high := visibleSections(y, height)
	for id := low; id <= high; id++ {
		if section := w.sections[id]; section != nil && section.terrain != nil {
			op := &ebiten.DrawImageOptions{}
			pixels := section.backgroundWidth()
			scale := float64(view.Pixels) / float64(pixels)
			op.GeoM.Translate(float64(render.BackgroundRasterBounds(pixels).Min.X), (terrain.SectionTop(id)-y)*float64(pixels))
			op.GeoM.Scale(scale, scale)
			op.GeoM.Translate(view.OffsetX, 0)
			w.appearanceFade.DrawReveal(dst, section.terrain, nil, section.backgroundReveal.Progress(), op.GeoM)
		}
	}
	// A vine is drawn once in world coordinates, even while crossing a seam.
	for id := max(0, low-1); id <= high+1; id++ {
		if section := w.sections[id]; section != nil {
			w.drawSupportedVegetation(dst, section.vines, section.vinesBounds, terrain.SectionWindowTop(id), y, height, section.plantWidth(backgroundVinesProduct), view, true, section.plantReveal(backgroundVinesProduct))
		}
	}
}

// Compose structural rocks and independent growth in their stable draw order.
// Runtime cuts mask images and replay dirty tiles before the next draw.
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
			w.drawRockSpan(dst, section, op.GeoM)
		}
	}
}

// Shadows retain complete grass, including ground patches, independently of hooks.
func (w *world) DrawForegroundSilhouette(dst *ebiten.Image, y, height, offsetX float64, view render.RenderTransform) {
	w.DrawForegroundRocks(dst, y, height, offsetX, view)
	w.drawGrass(dst, y, height, offsetX, view)
}

func (w *world) drawGrass(dst *ebiten.Image, y, height, offsetX float64, view render.RenderTransform) {
	low, high := visibleSections(y, height)
	for id := low; id <= high; id++ {
		section := w.sections[id]
		if section == nil || section.foreground == nil || section.grass == nil {
			continue
		}
		op := &ebiten.DrawImageOptions{}
		scale := float64(view.Pixels) / float64(section.grassWidth())
		op.GeoM.Scale(scale, scale)
		op.GeoM.Translate(view.OffsetX+offsetX*float64(view.Pixels), (terrain.SectionTop(id)-y)*float64(view.Pixels))
		w.appearanceFade.DrawReveal(dst, section.grass, nil, min(section.grassReveal.Progress(), section.foregroundReveal.Progress()), op.GeoM)
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
func (s *worldSection) needsRefresh(pixels int, layers render.MeshLayers) bool {
	if s.mesh != nil {
		// Partial recipes cannot refresh retained siblings whose inputs are absent.
		layers &= s.mesh.LayerMask()
		mask := s.mesh.LayerMask() & layers
		if mask&render.ForegroundLayer != 0 && s.foreground == nil {
			return true
		}
		if mask&render.ForegroundLayer != 0 && !s.mesh.ForegroundBaseOnly && s.needsGrass(*s.mesh, pixels) {
			return true
		}
		if mask&render.ForegroundLayer != 0 {
			for kind := growthProduct(0); kind < growthProducts; kind++ {
				if s.needsGrowth(*s.mesh, kind, pixels) {
					return true
				}
			}
		}
		if mask&render.BackgroundLayer != 0 && s.terrain == nil {
			return true
		}
		for kind := vegetationProduct(0); kind < vegetationProducts; kind++ {
			if mask&kind.layer() != 0 && s.needsPlant(*s.mesh, kind, pixels) {
				return true
			}
		}
	}
	return (layers&render.ForegroundLayer != 0 && s.foreground != nil && s.renderWidth() != pixels) ||
		(layers&render.BackgroundLayer != 0 && s.terrain != nil && s.backgroundWidth() != pixels) ||
		(layers&render.VegetationLayer != 0 && !s.vegetationPending && s.vegetationWidth() != pixels)
}

// Cross-band plants are visible only where their supporting rock layer exists.
// Adjacent clips partition the destination, preserving each stem's world origin.
func (w *world) drawSupportedVegetation(dst, layer *ebiten.Image, bounds image.Rectangle, top, y, height float64, pixels int, view render.RenderTransform, background bool, reveal *render.Reveal) {
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
		progress := reveal.Progress()
		if background {
			progress = min(progress, section.backgroundReveal.Progress())
		} else {
			progress = min(progress, section.foregroundReveal.Progress())
		}
		if w.appearanceFade == nil || progress >= 1 {
			render.DrawVegetation(dst.SubImage(clip).(*ebiten.Image), layer, bounds, top, y, pixels, view)
			continue
		}
		var transform ebiten.GeoM
		scale := float64(view.Pixels) / float64(pixels)
		transform.Translate(float64(bounds.Min.X), (top-y)*float64(pixels)+float64(bounds.Min.Y))
		transform.Scale(scale, scale)
		transform.Translate(view.OffsetX, 0)
		w.appearanceFade.DrawReveal(dst.SubImage(clip).(*ebiten.Image), layer, nil, progress, transform)
	}
}

func (w *world) BackgroundVisible(y, height float64) bool {
	if w.disableBackground {
		return false
	}
	lo, hi := visibleSections(y, height)
	for id := lo; id <= hi; id++ {
		if section := w.sections[id]; section != nil && section.terrain != nil {
			return true
		}
	}
	return false
}
