package infinicave

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Background faces, outlines, and material processing never wait for foreground.
func (w *world) advanceBackground(g *Scene, u *sectionUpload, budget *uploadBudget) {
	cursor := &u.backgroundCursor
	if cursor.stage == 0 {
		cursor.stage = uploadBackgroundFaces
	}
	top := terrain.SectionWindowTop(u.data.ID)
	for {
		switch cursor.stage {
		case uploadBackgroundFaces:
			if !budget.available() {
				return
			}
			if u.img == nil {
				u.raster.Background = render.ScaleGridMesh(u.data.Background, u.pixels, -render.BackgroundRasterBounds(u.pixels).Min.X)
				u.img = render.NewBackgroundImageAt(u.pixels)
				u.img.Fill(color.Black)
			}
			if !w.uploadFaces(g, cursor, u.img, u.raster.Background.Faces, top, true, budget) {
				return
			}
		case uploadBackgroundOutlines:
			if !w.uploadMeshes(cursor, u.img, u.raster.Background.Outlines, budget) {
				return
			}
		case uploadBackgroundFade:
			if !budget.enter(cursor.stage) || !budget.takeRaster(0, u.img.Bounds().Dx()*u.img.Bounds().Dy()) {
				return
			}
			faded := render.NewBackgroundImageAt(u.pixels)
			faded.DrawRectShader(u.img.Bounds().Dx(), u.img.Bounds().Dy(), g.backgroundFade, &ebiten.DrawRectShaderOptions{
				Images: [4]*ebiten.Image{u.img}, Uniforms: map[string]any{"Pixels": float32(u.pixels), "MinX": float32(float64(render.BackgroundRasterBounds(u.pixels).Min.X) / float64(u.pixels))},
			})
			u.img.Deallocate()
			u.img = faded
		case uploadBackgroundPublish:
			u.progress++
			section := w.sectionForUpload(u)
			if w.appearanceFade != nil && section.terrain == nil {
				section.backgroundReveal.Start(nil)
			}
			if section.terrain != nil {
				section.terrain.Deallocate()
			}
			section.terrain, u.img = u.img, nil
			section.backgroundPixels = u.pixels
			w.updateSectionSource(u)
			u.needed &^= render.BackgroundLayer
			if u.needed&render.ForegroundLayer == 0 {
				u.stage = u.firstStage()
			}
			return

		}
		cursor.stage++
		cursor.next = 0
	}
}
