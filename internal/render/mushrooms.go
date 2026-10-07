package render

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// One batched draw: per-mushroom vector paths allocated gigabytes per section.
func PrepareMushrooms(groups []terrain.MushroomGroup) TriangleMesh {
	var vertices []ebiten.Vertex
	var indices []uint32
	vertex := func(p geom.V, c color.NRGBA) uint32 {
		a := float32(c.A) / 255
		vertices = append(vertices, ebiten.Vertex{DstX: float32(p.X * RasterPixelsPerUnit), DstY: float32((p.Y - terrain.GenerationMinY) * RasterPixelsPerUnit), SrcX: .5, SrcY: .5,
			ColorR: float32(c.R) / 255 * a, ColorG: float32(c.G) / 255 * a, ColorB: float32(c.B) / 255 * a, ColorA: a})
		return uint32(len(vertices) - 1)
	}
	ribbon := func(points []geom.V, width float64, c color.NRGBA) {
		var prev [2]uint32
		for i, p := range points {
			n := points[min(len(points)-1, i+1)].Sub(points[max(0, i-1)]).Norm().Perp().Mul(width / 2)
			cur := [2]uint32{vertex(p.Add(n), c), vertex(p.Sub(n), c)}
			if i > 0 {
				indices = append(indices, prev[0], prev[1], cur[1], prev[0], cur[1], cur[0])
			}
			prev = cur
		}
	}
	fan := func(center geom.V, outline []geom.V, grow float64, c color.NRGBA) {
		mid := vertex(center, c)
		first := uint32(len(vertices))
		for _, p := range outline {
			vertex(p.Add(p.Sub(center).Norm().Mul(grow)), c)
		}
		for i := range outline {
			indices = append(indices, mid, first+uint32(i), first+uint32((i+1)%len(outline)))
		}
	}
	for _, group := range groups {
		for _, m := range group.Mushrooms {
			ribbon(m.Stem, m.CapWidth*.19, color.NRGBA{R: 46, G: 37, B: 28, A: 255})
			ribbon(m.Stem, m.CapWidth*.11, color.NRGBA{R: 216, G: 190, B: 143, A: 255})
			outline := terrain.MushroomCapOutline(m)
			center := m.CapCenter.Add(terrain.InternalPoint(terrain.MushroomOrientation(m), geom.V{X: 0, Y: -m.CapHeight * .4}))
			fan(center, outline, .001, color.NRGBA{R: 48, G: 36, B: 27, A: 255})
			fan(center, outline, 0, m.Color)
		}
	}
	return TriangleMesh{Vertices: vertices, Indices: indices}
}
