package main

import (
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// Shaded ribbons provide a dark outline, scarlet body and narrow longitudinal
// highlights. Shadows precede every body. Children inherit the parent shading
// at their attachment, so a dark ribbon edge cannot cut across a fork.
func drawVines(dst *ebiten.Image, vines []Vine) {
	white := ebiten.NewImage(1, 1)
	white.Fill(color.White)
	defer white.Deallocate()
	draw := func(index int, shadow bool) {
		vine := vines[index]
		var vertices []ebiten.Vertex
		var indices []uint32
		bands := vineBands[:]
		colors := vineColors[:]
		if shadow {
			bands = []float64{-1, 1}
			colors = []color.NRGBA{{0, 0, 0, 125}, {0, 0, 0, 125}}
		}
		for i, point := range vine.Points {
			before, after := vine.Points[max(0, i-1)].P, vine.Points[min(len(vine.Points)-1, i+1)].P
			n := after.Sub(before).Norm().Perp()
			for j, band := range bands {
				r := point.Radius
				p := point.P
				if shadow {
					r *= 1.2
					p = p.Add(V{1.4, 2})
				}
				p = p.Add(n.Mul(band * r))
				clr := colors[j]
				if !shadow {
					clr = vineJoinColor(vines, index, p, clr)
				}
				a := float32(clr.A) / 255
				vertices = append(vertices, ebiten.Vertex{DstX: float32(p.X), DstY: float32(p.Y), SrcX: .5, SrcY: .5, ColorR: float32(clr.R) / 255 * a, ColorG: float32(clr.G) / 255 * a, ColorB: float32(clr.B) / 255 * a, ColorA: a})
				if i > 0 && j > 0 {
					k := uint32(len(vertices) - 1)
					b := uint32(len(bands))
					indices = append(indices, k-b-1, k-b, k, k-b-1, k, k-1)
				}
			}
		}
		dst.DrawTriangles32(vertices, indices, white, &ebiten.DrawTrianglesOptions{AntiAlias: true})
	}
	for i := range vines {
		draw(i, true)
	}
	// Generation stores each parent before its children.
	for i := range vines {
		draw(i, false)
	}
}

var vineBands = [...]float64{-1, -.78, -.46, -.2, .04, .3, .65, 1}
var vineColors = [...]color.NRGBA{{42, 0, 3, 255}, {116, 1, 8, 255}, {200, 4, 13, 255}, {245, 16, 23, 255}, {210, 5, 12, 255}, {155, 1, 8, 255}, {92, 0, 5, 255}, {34, 0, 3, 255}}

func mixVineColor(a, b color.NRGBA, t float64) color.NRGBA {
	return color.NRGBA{
		R: uint8(math.Round(lerp(float64(a.R), float64(b.R), t))),
		G: uint8(math.Round(lerp(float64(a.G), float64(b.G), t))),
		B: uint8(math.Round(lerp(float64(a.B), float64(b.B), t))), A: 255,
	}
}

func vineBandColor(band float64) color.NRGBA {
	band = clamp(band, -1, 1)
	for i := 1; i < len(vineBands); i++ {
		if band <= vineBands[i] {
			return mixVineColor(vineColors[i-1], vineColors[i], (band-vineBands[i-1])/(vineBands[i]-vineBands[i-1]))
		}
	}
	return vineColors[len(vineColors)-1]
}

// Match the parent's material across the entire root cross-section and blend
// into the child's own highlight as it leaves the junction. This also makes
// a twig inherit the already blended material of its branch.
func vineJoinColor(vines []Vine, index int, p V, own color.NRGBA) color.NRGBA {
	vine := vines[index]
	if vine.Depth == 0 || vine.Parent < 0 || vine.Parent >= index {
		return own
	}
	parent := vines[vine.Parent]
	root := parent.Points[vine.Joint]
	if root.Radius <= 0 {
		return own
	}
	blend := smoothstep(root.Radius, root.Radius*4, p.Sub(root.P).Len())
	if blend >= 1 {
		return own
	}
	before := parent.Points[max(0, vine.Joint-1)].P
	after := parent.Points[min(len(parent.Points)-1, vine.Joint+1)].P
	normal := after.Sub(before).Norm().Perp()
	band := p.Sub(root.P).Dot(normal) / root.Radius
	inherited := vineJoinColor(vines, vine.Parent, p, vineBandColor(band))
	return mixVineColor(inherited, own, blend)
}
