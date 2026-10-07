package render

import (
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func foregroundVinePalette(vines []terrain.Vine, index int) [len(vineColors)]color.NRGBA {
	// Copper highlights and rust bodies sit inside a dark bark outline.
	tints := [...][3]float64{{1, 1, 1}, {.97, .95, 1.04}, {1, 1.05, .96}, {.96, .98, 1.06}}
	tint := tints[terrain.VineStyleFamily(vines, index)%len(tints)]
	palette := [len(vineColors)]color.NRGBA{
		{34, 22, 18, 255}, {34, 22, 18, 255}, {183, 104, 60, 255}, {183, 104, 60, 255},
		{115, 55, 33, 255}, {115, 55, 33, 255}, {34, 22, 18, 255}, {34, 22, 18, 255},
	}
	for i, c := range palette {
		palette[i] = color.NRGBA{
			uint8(math.Round(float64(c.R) * tint[0])),
			uint8(math.Round(float64(c.G) * tint[1])),
			uint8(math.Round(float64(c.B) * tint[2])), 255,
		}
	}
	return palette
}

func PrepareForegroundVines(vines []terrain.Vine, orientation ...terrain.Orientation) []TriangleMesh {
	shadowOffset := terrain.InternalPoint(terrain.OptionalOrientation(orientation), geom.V{X: 1, Y: 1.5})
	if len(vines) == 0 {
		return nil
	}
	stems := PrepareVines(vines)
	meshes := make([]TriangleMesh, 0, len(stems)*2)
	// A small contact shadow seats the crisp ribbons on the raised surface.
	// Draw all shadows first so they cannot darken a neighboring vine.
	for _, stem := range stems {
		vertices := append([]ebiten.Vertex(nil), stem.Vertices...)
		for i := range vertices {
			v := &vertices[i]
			v.DstX += float32(shadowOffset.X)
			v.DstY += float32(shadowOffset.Y)
			v.ColorR, v.ColorG, v.ColorB, v.ColorA = 0, 0, 0, .38
		}
		meshes = append(meshes, TriangleMesh{Vertices: vertices, Indices: stem.Indices, premultiplied: true})
	}
	return append(meshes, stems...)
}
