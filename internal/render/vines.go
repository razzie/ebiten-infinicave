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
func PrepareVines(vines []terrain.Vine) []TriangleMesh {
	meshes := make([]TriangleMesh, 0, len(vines))
	prepare := func(index int) {
		vine := vines[index]
		var vertices []ebiten.Vertex
		var indices []uint32
		palette := vinePalette(vines, index)
		for i, point := range vine.Points {
			before, after := vine.Points[max(0, i-1)].P, vine.Points[min(len(vine.Points)-1, i+1)].P
			n := after.Sub(before).Norm().Perp()
			for j, band := range vineBands {
				p := point.P.Add(n.Mul(band * point.Radius))
				clr := weatherVineColor(vineJoinColor(vines, index, p, palette[j]), p, terrain.VineStyleFamily(vines, index))
				a := float32(clr.A) / 255
				vertices = append(vertices, ebiten.Vertex{DstX: float32(p.X * RasterPixelsPerUnit), DstY: float32((p.Y - terrain.GenerationMinY) * RasterPixelsPerUnit), SrcX: .5, SrcY: .5, ColorR: float32(clr.R) / 255 * a, ColorG: float32(clr.G) / 255 * a, ColorB: float32(clr.B) / 255 * a, ColorA: a})
				if i > 0 && j > 0 && vineBands[j] != vineBands[j-1] {
					k := uint32(len(vertices) - 1)
					b := uint32(len(vineBands))
					indices = append(indices, k-b-1, k-b, k, k-b-1, k, k-1)
				}
			}
		}
		meshes = append(meshes, TriangleMesh{Vertices: vertices, Indices: indices})
	}
	// Generation stores each parent before its children.
	for i := range vines {
		prepare(i)
	}
	return meshes
}

// Paired vertices share positions at each tonal boundary, keeping the bands
// crisp instead of interpolating a rounded, glossy cylinder across the stem.
var vineBands = [...]float64{-1, -.78, -.78, -.30, -.30, .78, .78, 1}

var vineColors = [...]color.NRGBA{
	{34, 0, 3, 255}, {34, 0, 3, 255},
	{245, 16, 23, 255}, {245, 16, 23, 255},
	{155, 1, 8, 255}, {155, 1, 8, 255},
	{34, 0, 3, 255}, {34, 0, 3, 255},
}

func vinePalette(vines []terrain.Vine, index int) [len(vineColors)]color.NRGBA {
	if vines[terrain.VineFamily(vines, index)].Foreground {
		return foregroundVinePalette(vines, index)
	}
	type paletteStyle struct {
		tint                   [3]int
		grayMix, brownMix, dim float64
	}
	styles := [...]paletteStyle{
		{[3]int{0, 0, 0}, .92, 0, .36},
		{[3]int{7, 2, -3}, .92, 0, .36},
		{[3]int{-4, 1, 5}, .92, 0, .36},
		{[3]int{12, -4, -6}, .92, 0, .36},
		{[3]int{-8, 5, 11}, .92, 0, .36},
		{[3]int{5, 2, -3}, .92, 0, .36},
		{[3]int{-5, -2, 10}, .92, 0, .36},
		{[3]int{9, -3, 0}, .92, 0, .36},
		{[3]int{0, 0, 0}, .92, .31, .36},
		{[3]int{0, 0, 0}, .92, .42, .36},
		{[3]int{0, 0, 0}, 1, 0, .34},
		{[3]int{0, 0, 0}, 1, 0, .39},
	}
	var palette [len(vineColors)]color.NRGBA
	family := terrain.VineStyleFamily(vines, index)
	style := styles[family%len(styles)]
	for i, base := range vineColors {
		gray := .30*float64(base.R) + .59*float64(base.G) + .11*float64(base.B)
		shade := func(channel uint8, tint int, sepia float64) uint8 {
			value := geom.Lerp(float64(channel), gray, style.grayMix)
			value = geom.Lerp(value, gray*sepia, style.brownMix)
			return uint8(geom.Clamp(math.Round((value+float64(tint))*style.dim), 0, 255))
		}
		palette[i] = color.NRGBA{
			R: shade(base.R, style.tint[0], 1.12),
			G: shade(base.G, style.tint[1], .82),
			B: shade(base.B, style.tint[2], .58),
			A: 255,
		}
	}
	return palette
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
	inherited := vineJoinColor(vines, vine.Parent, p, vineBandColorFrom(vinePalette(vines, vine.Parent), band))
	return mixVineColor(inherited, own, blend)
}
