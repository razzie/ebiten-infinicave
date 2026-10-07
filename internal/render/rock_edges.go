package render

import (
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func appendRockQuad(vertices []ebiten.Vertex, indices []uint32, points [4]geom.V, clr color.NRGBA) ([]ebiten.Vertex, []uint32) {
	first := uint32(len(vertices))
	for _, p := range points {
		vertices = append(vertices, ebiten.Vertex{
			DstX: float32(p.X * RasterPixelsPerUnit), DstY: float32((p.Y - terrain.GenerationMinY) * RasterPixelsPerUnit), SrcX: float32(p.X * RasterPixelsPerUnit), SrcY: float32((p.Y - terrain.GenerationMinY) * RasterPixelsPerUnit),
			ColorR: float32(clr.R) / 255, ColorG: float32(clr.G) / 255, ColorB: float32(clr.B) / 255, ColorA: float32(clr.A) / 255,
			Custom0: 2,
		})
	}
	return vertices, append(indices, first, first+1, first+2, first, first+2, first+3)
}

// A shallow oblique view exposes the drop below a silhouette. Draw walls
// before the top faces; their inward portions are naturally hidden by rock.
func appendRockWalls(vertices []ebiten.Vertex, indices []uint32, grid terrain.RockGrid, edges []terrain.RockEdge, viewMode View) ([]ebiten.Vertex, []uint32) {
	if len(grid) == 0 || !grid[0].Raised {
		return vertices, indices
	}
	for _, edge := range edges {
		c := grid[edge.Cell]
		outward := edge.B.Sub(edge.A).Perp().Norm().Mul(-1)
		view := terrain.InternalPoint(terrain.RockOrientation(c), geom.V{X: .045, Y: .08})
		if outward.Dot(view) <= 0 {
			continue
		}
		za, zb := math.Max(0, terrain.RockDepthAt(c, edge.A)+.008), math.Max(0, terrain.RockDepthAt(c, edge.B)+.008)
		a := edge.A.Add(view.Mul(za))
		b := edge.B.Add(view.Mul(zb))
		normal := (geom.V3{X: outward.X, Y: outward.Y, Z: .1}).Norm()
		clr := terrain.RockSurfaceColor(normal, c.Shadow*.65, c.Ambient*.55, terrain.RockOrientation(c))
		clr = rockViewColor(terrain.RockCell{Color: clr, Raised: true}, viewMode)
		vertices, indices = appendRockQuad(vertices, indices, [4]geom.V{edge.A, edge.B, b, a}, clr)
	}
	return vertices, indices
}

func rockViewColor(c terrain.RockCell, view View) color.NRGBA {
	switch view {
	case ViewHeight:
		v := uint8(math.Round(255 * geom.Clamp((c.Z+.012)/.112, 0, 1)))
		return color.NRGBA{v, v, v, 255}
	case ViewNormals:
		n := terrain.WorldNormal(terrain.RockOrientation(c), c.Normal)
		return color.NRGBA{uint8(127.5 * (n.X + 1)), uint8(127.5 * (n.Y + 1)), uint8(127.5 * (n.Z + 1)), 255}
	case ViewShadows:
		v := uint8(math.Round(255 * c.Shadow))
		return color.NRGBA{v, v, v, 255}
	case ViewClay:
		v := uint8(math.Round(.30*float64(c.Color.R) + .59*float64(c.Color.G) + .11*float64(c.Color.B)))
		return color.NRGBA{v, v, v, c.Color.A}
	}
	return rockMaterialColor(c.Color, c.Raised)
}

// Grade only the rendered material. Terrain colors also steer vine growth,
// so changing their palette during generation would change seeded geometry.
func rockMaterialColor(c color.NRGBA, raised bool) color.NRGBA {
	if !raised {
		v := (.30*float64(c.R) + .59*float64(c.G) + .11*float64(c.B)) / 31
		return color.NRGBA{
			R: uint8(math.Round(math.Min(255, 19*v))),
			G: uint8(math.Round(math.Min(255, 26*v))),
			B: uint8(math.Round(math.Min(255, 29*v))), A: c.A,
		}
	}
	stops := [...]struct {
		red, r, g, b float64
	}{
		{0, 0, 0, 0}, {8, 8, 8, 7}, {18, 23, 22, 20},
		{38, 49, 46, 40}, {120, 151, 139, 116},
		{165, 229, 210, 172}, {190, 255, 237, 197}, {255, 255, 250, 230},
	}
	for i := 1; i < len(stops); i++ {
		a, b := stops[i-1], stops[i]
		if float64(c.R) <= b.red {
			t := (float64(c.R) - a.red) / (b.red - a.red)
			return color.NRGBA{
				R: uint8(math.Round(geom.Lerp(a.r, b.r, t))),
				G: uint8(math.Round(geom.Lerp(a.g, b.g, t))),
				B: uint8(math.Round(geom.Lerp(a.b, b.b, t))), A: c.A,
			}
		}
	}
	return c
}

// A restrained bevel belongs only to an exposed silhouette. Internal cell
// edges have neither a raised rim nor a lighting seam from triangulation.
func appendRockBevels(vertices []ebiten.Vertex, indices []uint32, grid terrain.RockGrid, edges []terrain.RockEdge, view View) ([]ebiten.Vertex, []uint32) {
	for _, edge := range edges {
		c := grid[edge.Cell]
		inward := edge.B.Sub(edge.A).Perp().Norm()
		width := math.Min(.00045, edge.B.Sub(edge.A).Len()*.06)
		a, b := edge.A.Add(inward.Mul(width)), edge.B.Add(inward.Mul(width))
		if !geom.InsidePolygon(geom.LerpVector(a, b, .5), c.Polygon) {
			continue
		}
		normal := (geom.V3{X: c.Normal.X - inward.X*.5, Y: c.Normal.Y - inward.Y*.5, Z: c.Normal.Z}).Norm()
		clr := terrain.RockSurfaceColor(normal, c.Shadow, c.Ambient, terrain.RockOrientation(c))
		clr = rockViewColor(terrain.RockCell{Color: clr, Raised: true}, view)
		if view == ViewShaded {
			// Color the existing bevel with warm grazing light. Shadowed edges
			// retain their material tone, so the rim never becomes an outline.
			light := terrain.SurfaceLight(normal, terrain.RockOrientation(c))
			warmth := .72 * light * light * c.Shadow
			clr.R = uint8(math.Round(geom.Lerp(float64(clr.R), 255, warmth)))
			clr.G = uint8(math.Round(geom.Lerp(float64(clr.G), 199, warmth)))
			clr.B = uint8(math.Round(geom.Lerp(float64(clr.B), 112, warmth)))
		}
		vertices, indices = appendRockQuad(vertices, indices, [4]geom.V{edge.A, edge.B, b, a}, clr)
	}
	return vertices, indices
}
