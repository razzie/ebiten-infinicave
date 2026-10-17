package render

import (
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Ribbons have flat highlight and body bands with dark outlines. Children inherit the
// parent shading at their attachment, so a ribbon edge cannot cut across a fork.
// All stems precede the leaf pass so later vines cannot cover earlier leaves.
func PrepareVines(vines []terrain.Vine) []TriangleMesh {
	meshes := make([]TriangleMesh, 0, len(vines))
	// Generation stores each parent before its children.
	for index := range vines {
		vine := vines[index]
		var vertices []ebiten.Vertex
		var indices []uint32
		along := 0.0
		for i, point := range vine.Points {
			if i > 0 {
				along += point.P.Sub(vine.Points[i-1].P).Len()
			}
			distance := along
			if len(vine.Along) == len(vine.Points) {
				distance = vine.Along[i]
			}
			palette := vinePaletteAtPoint(vines, index, point.P, distance)
			before, after := vine.Points[max(0, i-1)].P, vine.Points[min(len(vine.Points)-1, i+1)].P
			n := after.Sub(before).Norm().Perp()
			for j, band := range vineBands {
				p := point.P.Add(n.Mul(band * point.Radius))
				clr := weatherVineColor(vineJoinColor(vines, index, p, palette[j]), p, terrain.VineStyleFamily(vines, index))
				vertices = append(vertices, vineVertex(p, clr))
				if i > 0 && j > 0 && vineBands[j] != vineBands[j-1] {
					k := uint32(len(vertices) - 1)
					b := uint32(len(vineBands))
					indices = append(indices, k-b-1, k-b, k, k-b-1, k, k-1)
				}
			}
		}
		meshes = append(meshes, TriangleMesh{Vertices: vertices, Indices: indices})
	}
	for i := range vines {
		if len(vines[i].Leaves) == 0 {
			continue
		}
		var leaves TriangleMesh
		appendVineLeaves(vines, i, &leaves.Vertices, &leaves.Indices)
		meshes = append(meshes, leaves)
	}
	return meshes
}

// Bake local rock lighting into the immutable recipe so uploads, cut replay,
// and resizing all retain the same shading without sampling cached images.
func prepareRockVines(vines []terrain.Vine, grid terrain.RockGrid) []TriangleMesh {
	meshes := PrepareVines(vines)
	if len(vines) == 0 || len(grid) == 0 {
		return meshes
	}
	bounds := make([]geom.Bounds, len(grid))
	shades := make([]float32, len(grid))
	for i, cell := range grid {
		if len(cell.Polygon) < 3 {
			continue
		}
		bounds[i].Min, bounds[i].Max = geom.PolygonBounds(cell.Polygon)
		clr := rockViewColor(cell, ViewShaded)
		brightness := .30*float64(clr.R) + .59*float64(clr.G) + .11*float64(clr.B)
		white, floor, contrast := 45.0, .85, .40
		if cell.Raised {
			white = 180
			floor, contrast = .38, .97
		}
		// Keep recessed vines near their original muted palette brightness,
		// with gentler cell contrast than the raised foreground vines.
		shades[i] = float32(floor + contrast*geom.Smoothstep(0, white, brightness))
	}
	index := geom.NewBoundsIndex(bounds)
	for _, mesh := range meshes {
		for i := range mesh.Vertices {
			v := &mesh.Vertices[i]
			p := geom.V{X: float64(v.DstX) / RasterPixelsPerUnit, Y: float64(v.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY}
			index.Query(geom.Bounds{Min: p, Max: p}, func(cell int) bool {
				if len(grid[cell].Polygon) < 3 || grid[cell].Color.A == 0 || !geom.InsidePolygon(p, grid[cell].Polygon) {
					return true
				}
				shade := shades[cell]
				v.ColorR = min(v.ColorR*shade, v.ColorA)
				v.ColorG = min(v.ColorG*shade, v.ColorA)
				v.ColorB = min(v.ColorB*shade, v.ColorA)
				return false
			})
		}
	}
	return meshes
}

// Paired vertices share positions at each tonal boundary, keeping the bands
// crisp instead of interpolating a rounded, glossy cylinder across the stem.
var vineBands = [...]float64{-1, -.78, -.78, -.30, -.30, .78, .78, 1}

// Sage and straw highlights sit over dusty bodies and tinted bark.
var vineMaterials = [...][3]color.NRGBA{
	{{46, 58, 45, 255}, {174, 194, 162, 255}, {108, 132, 96, 255}},
	{{65, 55, 38, 255}, {220, 200, 145, 255}, {160, 136, 91, 255}},
}

var vineColors = expandVineMaterial(vineMaterials[0])

func expandVineMaterial(material [3]color.NRGBA) [len(vineBands)]color.NRGBA {
	return [len(vineBands)]color.NRGBA{material[0], material[0], material[1], material[1], material[2], material[2], material[0], material[0]}
}

func vinePalette(vines []terrain.Vine, index int) [len(vineBands)]color.NRGBA {
	return vinePaletteAt(vines, index, 0)
}

func vinePaletteAt(vines []terrain.Vine, index int, distance float64) [len(vineBands)]color.NRGBA {
	var p geom.V
	if len(vines[index].Points) > 0 {
		p = vines[index].Points[0].P
	}
	return vinePaletteAtPoint(vines, index, p, distance)
}

func vinePaletteAtPoint(vines []terrain.Vine, index int, p geom.V, distance float64) [len(vineBands)]color.NRGBA {
	style := terrain.VineMaterial(vines, index)
	vine := vines[index]
	vine.Style = style
	green := terrain.VineGreenAt(vine, p, distance)
	a := expandVineMaterial(vineMaterials[terrain.VineStraw])
	b := expandVineMaterial(vineMaterials[terrain.VineSage])
	// Keep depth readable without washing both hues into charcoal.
	dim := 1.0
	if !vines[terrain.VineFamily(vines, index)].Foreground {
		dim = .20
	}
	for i := range a {
		c := mixVineColor(a[i], b[i], green)
		shade := func(channel uint8) uint8 {
			return uint8(geom.Clamp(math.Round(float64(channel)*dim*(1+style.Tint)), 0, 255))
		}
		a[i] = color.NRGBA{shade(c.R), shade(c.G), shade(c.B), 255}
	}
	return a
}

func vineVertex(p geom.V, clr color.NRGBA) ebiten.Vertex {
	a := float32(clr.A) / 255
	return ebiten.Vertex{DstX: float32(p.X * RasterPixelsPerUnit), DstY: float32((p.Y - terrain.GenerationMinY) * RasterPixelsPerUnit), SrcX: .5, SrcY: .5, ColorR: float32(clr.R) / 255 * a, ColorG: float32(clr.G) / 255 * a, ColorB: float32(clr.B) / 255 * a, ColorA: a}
}

func appendVineLeaves(vines []terrain.Vine, index int, vertices *[]ebiten.Vertex, indices *[]uint32) {
	for _, leaf := range vines[index].Leaves {
		palette := vinePaletteAtPoint(vines, index, leaf.Anchor, leaf.Along)
		// Two flat halves meet at a root-to-tip vein, like folded paper.
		for half := range 2 {
			shoulder, shade := 1, palette[2]
			if half == 1 {
				shoulder, shade = 3, palette[4]
			}
			base := uint32(len(*vertices))
			for _, point := range []int{0, shoulder, 2} {
				p := leaf.Polygon[point]
				*vertices = append(*vertices, vineVertex(p, weatherVineColor(shade, p, terrain.VineStyleFamily(vines, index))))
			}
			*indices = append(*indices, base, base+1, base+2)
		}
	}
}

func weatherVineColor(clr color.NRGBA, p geom.V, family int) color.NRGBA {
	familyOffset := float64(family)
	grain := .5 + .25*math.Sin(p.X*71+(p.Y-terrain.GenerationMinY)*37+familyOffset*1.7) + .25*math.Sin(p.X*29-(p.Y-terrain.GenerationMinY)*83+familyOffset*.61)
	factor := .98 + .02*grain
	return color.NRGBA{
		R: uint8(math.Round(float64(clr.R) * factor)),
		G: uint8(math.Round(float64(clr.G) * factor)),
		B: uint8(math.Round(float64(clr.B) * factor)),
		A: clr.A,
	}
}

func mixVineColor(a, b color.NRGBA, t float64) color.NRGBA {
	return color.NRGBA{
		R: uint8(math.Round(geom.Lerp(float64(a.R), float64(b.R), t))),
		G: uint8(math.Round(geom.Lerp(float64(a.G), float64(b.G), t))),
		B: uint8(math.Round(geom.Lerp(float64(a.B), float64(b.B), t))), A: 255,
	}
}

func vineBandColor(band float64) color.NRGBA {
	return vineBandColorFrom(vineColors, band)
}

func vineBandColorFrom(palette [len(vineColors)]color.NRGBA, band float64) color.NRGBA {
	band = geom.Clamp(band, -1, 1)
	for i := 1; i < len(vineBands); i++ {
		if band <= vineBands[i] {
			return mixVineColor(palette[i-1], palette[i], (band-vineBands[i-1])/(vineBands[i]-vineBands[i-1]))
		}
	}
	return palette[len(palette)-1]
}

// Match the parent's material across the entire root cross-section and blend
// into the child's own highlight as it leaves the junction. This also makes
// a twig inherit the already blended material of its branch.
func vineJoinColor(vines []terrain.Vine, index int, p geom.V, own color.NRGBA) color.NRGBA {
	vine := vines[index]
	if vine.Depth == 0 || vine.Parent < 0 || vine.Parent >= index {
		return own
	}
	parent := vines[vine.Parent]
	root := parent.Points[vine.Joint]
	if root.Radius <= 0 {
		return own
	}
	blend := geom.Smoothstep(root.Radius, root.Radius*4, p.Sub(root.P).Len())
	if blend >= 1 {
		return own
	}
	before := parent.Points[max(0, vine.Joint-1)].P
	after := parent.Points[min(len(parent.Points)-1, vine.Joint+1)].P
	normal := after.Sub(before).Norm().Perp()
	band := p.Sub(root.P).Dot(normal) / root.Radius
	inherited := vineJoinColor(vines, vine.Parent, p, vineBandColorFrom(vinePaletteAtPoint(vines, vine.Parent, root.P, terrain.VineDistance(parent, vine.Joint)), band))
	return mixVineColor(inherited, own, blend)
}

func PrepareForegroundVines(vines []terrain.Vine, orientation ...terrain.Orientation) []TriangleMesh {
	return prepareForegroundVines(vines, nil, terrain.OptionalOrientation(orientation))
}

func prepareForegroundVines(vines []terrain.Vine, grid terrain.RockGrid, orientation terrain.Orientation) []TriangleMesh {
	shadowOffset := terrain.InternalPoint(orientation, geom.V{X: 1, Y: 1.5})
	if len(vines) == 0 {
		return nil
	}
	vineMeshes := prepareRockVines(vines, grid)
	meshes := make([]TriangleMesh, 0, len(vineMeshes)*2)
	// A small contact shadow seats the crisp ribbons on the raised surface.
	// Draw all shadows first so they cannot darken a neighboring vine.
	for _, mesh := range vineMeshes {
		vertices := append([]ebiten.Vertex(nil), mesh.Vertices...)
		for i := range vertices {
			v := &vertices[i]
			v.DstX += float32(shadowOffset.X)
			v.DstY += float32(shadowOffset.Y)
			v.ColorR, v.ColorG, v.ColorB, v.ColorA = 0, 0, 0, .38
		}
		meshes = append(meshes, TriangleMesh{Vertices: vertices, Indices: mesh.Indices, premultiplied: true})
	}
	return append(meshes, vineMeshes...)
}
