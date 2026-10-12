package render

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

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
