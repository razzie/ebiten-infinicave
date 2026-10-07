package infinicave

import (
	"image"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Prepare fresh meshes before cropping; finishSectionMesh translates vertices
// in place, so an already cropped mesh must never be finished a second time.
func (g *Scene) prepareCarvedVegetation(mesh *render.SectionMesh) {
	mesh.Vines, mesh.ForegroundVines, mesh.Mushrooms = nil, nil, render.TriangleMesh{}
	if g.view == ViewShaded {
		plants := mesh.Geometry.Vegetation
		mesh.Vines = render.PrepareVines(plants.Vines)
		mesh.ForegroundVines = render.PrepareForegroundVines(plants.ForegroundVines, g.orientation)
		mesh.Mushrooms = render.PrepareMushrooms(plants.Mushrooms)
	}
	render.FinishSectionMesh(mesh)
}

func (g *Scene) redrawCarvedVegetation(section *worldSection, id int64) {
	if section.vegetationPending || section.terrain == nil {
		return
	}
	mesh := render.SectionMesh{ID: id, Geometry: section.geometry}
	g.prepareCarvedVegetation(&mesh)
	if section.mesh != nil {
		section.mesh.Geometry = section.geometry
		section.mesh.Vines, section.mesh.ForegroundVines, section.mesh.Mushrooms = mesh.Vines, mesh.ForegroundVines, mesh.Mushrooms
		section.mesh.VinesBounds, section.mesh.ForegroundVinesBounds, section.mesh.MushroomsBounds = mesh.VinesBounds, mesh.ForegroundVinesBounds, mesh.MushroomsBounds
	}
	pixels := section.renderWidth()
	mesh = render.ScaleSectionMesh(mesh, pixels)
	if g.world.white == nil {
		g.world.white = ebiten.NewImage(1, 1)
		g.world.white.Fill(color.White)
	}
	for _, layer := range []*ebiten.Image{section.vines, section.foregroundVines, section.mushrooms} {
		if layer != nil {
			layer.Deallocate()
		}
	}
	draw := func(meshes []render.TriangleMesh, bounds image.Rectangle) *ebiten.Image {
		if bounds.Empty() {
			return nil
		}
		img := render.NewVegetationImage(bounds)
		for _, m := range meshes {
			m.Draw(img, g.world.white)
		}
		return img
	}
	section.vines = draw(mesh.Vines, mesh.VinesBounds)
	section.foregroundVines = draw(mesh.ForegroundVines, mesh.ForegroundVinesBounds)
	section.mushrooms = draw([]render.TriangleMesh{mesh.Mushrooms}, mesh.MushroomsBounds)
	section.vinesBounds, section.foregroundVinesBounds, section.mushroomsBounds = mesh.VinesBounds, mesh.ForegroundVinesBounds, mesh.MushroomsBounds
	top := terrain.SectionWindowTop(id)
	section.vines = g.finishVinesAt(section.vines, section.vinesBounds, top, pixels)
	for _, layer := range []struct {
		image  *ebiten.Image
		bounds image.Rectangle
	}{
		{section.vines, section.vinesBounds}, {section.foregroundVines, section.foregroundVinesBounds},
	} {
		render.EraseVineCutsAt(layer.image, layer.bounds, top, section.geometry.Vegetation.Cuts, pixels)
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

func (g *Scene) redrawCarvedSection(section *worldSection) {
	if section.foreground == nil { // CPU-only geometry consumers/tests
		return
	}
	mesh := render.PrepareGridWithTopology(nil, g.view, section.geometry.Topology)
	if section.mesh != nil {
		section.mesh.Foreground = mesh
		section.mesh.Geometry = section.geometry
	}
	g.drawCarvedForeground(section.foreground, mesh, section.geometry.Top)
}

func (g *Scene) drawCarvedForeground(dst *ebiten.Image, mesh render.GridMesh, top float64) {
	pixels := dst.Bounds().Dx()
	mesh = render.ScaleGridMesh(mesh, pixels, 0)
	dst.Clear()
	g.drawGridFaces(dst, mesh.Faces, top-SectionHeight)
	if g.world.white == nil {
		g.world.white = ebiten.NewImage(1, 1)
		g.world.white.Fill(color.White)
	}
	for _, outline := range mesh.Outlines {
		outline.Draw(dst, g.world.white)
	}
}

func (g *Scene) applyStoredCuts(mesh *render.SectionMesh) {
	changed := false
	plantsChanged := false
	for _, cut := range g.world.cuts {
		geometry, _, edited := cut.Geometry(mesh.ID, mesh.Geometry, g.collisionTolerance)
		geometry, plantsEdited := cut.Plants(geometry)
		mesh.Geometry = geometry
		changed = changed || edited
		plantsChanged = plantsChanged || plantsEdited
	}
	if changed {
		mesh.Foreground = render.PrepareGridWithTopology(nil, g.view, mesh.Geometry.Topology)
	}
	if plantsChanged {
		g.prepareCarvedVegetation(mesh)
	}
}

// Restart affected uploads, including partially drawn or blurred vegetation.
// Published foreground was edited above and need not be uploaded again.
func (g *Scene) carveUpload(cut terrain.RockCut) {
	u := g.world.upload
	if u == nil {
		return
	}
	geometry, _, changed := cut.Geometry(u.data.ID, u.data.Geometry, g.collisionTolerance)
	geometry, plantsChanged := cut.Plants(geometry)
	if !changed && !plantsChanged {
		return
	}
	defer func() {
		pixels := u.pixels
		if pixels == 0 {
			pixels = g.world.renderWidth()
		}
		u.raster = render.ScaleSectionMesh(u.data, pixels)
	}()
	u.data.Geometry = geometry
	if changed {
		u.data.Foreground = render.PrepareGridWithTopology(nil, g.view, geometry.Topology)
	}
	if plantsChanged {
		g.prepareCarvedVegetation(&u.data)
		for _, img := range []*ebiten.Image{u.vines, u.foregroundVines, u.mushrooms} {
			if img != nil {
				img.Deallocate()
			}
		}
		u.vines, u.foregroundVines, u.mushrooms = nil, nil, nil
		if u.stage >= 5 {
			u.stage, u.next = 5, 0
			if section := g.world.sections[u.data.ID]; section != nil {
				section.geometry = geometry
			}
		}
	}
	if u.stage >= 5 {
		return
	}
	if !changed {
		return
	}
	if u.stage >= 2 {
		if u.foreground != nil {
			u.foreground.Deallocate()
			u.foreground = nil
		}
		u.stage, u.next = 2, 0
	}
}
