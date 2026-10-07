package render

import (
	"image"

	"github.com/hajimehoshi/ebiten/v2"
)

// Keep cached render targets out of the atlas: reallocation must not change
// their raster origin and introduce subpixel differences on revisiting.
func NewSectionImageAt(height, pixels int) *ebiten.Image {
	return ebiten.NewImageWithOptions(image.Rect(0, 0, pixels, height*pixels), &ebiten.NewImageOptions{Unmanaged: true})
}

func DrawVegetation(dst, layer *ebiten.Image, bounds image.Rectangle, top, y float64, pixels int, view RenderTransform) {
	if layer == nil {
		return
	}
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(bounds.Min.X), (top-y)*float64(pixels)+float64(bounds.Min.Y))
	scale := float64(view.Pixels) / float64(pixels)
	op.GeoM.Scale(scale, scale)
	op.GeoM.Translate(view.OffsetX, 0)
	dst.DrawImage(layer, op)
}

func NewVegetationImage(bounds image.Rectangle) *ebiten.Image {
	return ebiten.NewImageWithOptions(image.Rect(0, 0, bounds.Dx(), bounds.Dy()), &ebiten.NewImageOptions{Unmanaged: true})
}
