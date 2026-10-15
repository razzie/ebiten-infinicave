package infinicave

import (
	"image"

	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

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
	bounds := image.Rect(0, u.pixels, u.pixels, 2*u.pixels)
	g.eraseCuts(u.grass, bounds, terrain.SectionWindowTop(data.ID), data.VisualCuts, u.pixels)
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
