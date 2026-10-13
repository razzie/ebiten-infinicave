package infinicave

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
)

// NewShader prepares Kage, but the driver compiles its GPU program on first
// use. Submit transparent one-pixel draws while CPU generation is underway,
// rather than delaying the first completed terrain and vine uploads.
func (g *Scene) warmMaterials(dst *ebiten.Image) {
	if g.materialsWarm {
		return
	}
	g.materialsWarm = true
	mesh := render.TriangleMesh{
		Vertices: []ebiten.Vertex{{DstX: 0, DstY: 0}, {DstX: 1, DstY: 0}, {DstX: 0, DstY: 1}},
		Indices:  []uint32{0, 1, 2},
	}
	if g.material != nil {
		g.drawGridFacesWithAA(dst, mesh, 0, false)
	}
	if g.vineMaterial != nil {
		blank := ebiten.NewImage(1, 1)
		defer blank.Deallocate()
		dst.DrawRectShader(1, 1, g.vineMaterial, &ebiten.DrawRectShaderOptions{
			Images:   [4]*ebiten.Image{blank},
			Uniforms: map[string]any{"Texture": float32(g.texture / 8), "Offset": []float32{0, 0}, "Scale": float32(1)},
		})
	}
}
