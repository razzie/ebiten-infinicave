package infinicave

import (
	"image/color"
	"math"
	"math/rand"

	"github.com/hajimehoshi/ebiten/v2"
)

const foregroundVineGuideClearance = 30.0

func newForegroundVineTerrain(foreground RockGrid, guides []Guide) *VineTerrain {
	// Growth is based on the visible footprint, including its screen inset,
	// rather than rock brightness: shaded and lit faces both support vines.
	support := insetForegroundGrid(foreground)
	for i := range support {
		if support[i].Color.A != 0 {
			support[i].Color = color.NRGBA{36, 36, 36, 255}
		}
	}
	field := newVineTerrain(support, nil)
	field.onForeground = true
	// Restrict the same clearance field used by root placement, steering,
	// and seam-following twigs so the full family keeps off the crests.
	// The existing conservative sampler also reserves room for ribbon width.
	const reach = foregroundVineGuideClearance + vineBorderRange
	for _, guide := range guides {
		for j := 1; j < len(guide.Pts); j++ {
			a, b := guide.Pts[j-1], guide.Pts[j]
			d := b.Sub(a)
			length2 := d.Len2()
			if length2 < 1e-12 {
				continue
			}
			x0 := max(0, int(math.Floor((math.Min(a.X, b.X)-reach)/vineFieldStep)))
			x1 := min(vineFieldWidth-1, int(math.Ceil((math.Max(a.X, b.X)+reach)/vineFieldStep)))
			y0 := max(0, int(math.Floor((math.Min(a.Y, b.Y)-reach)/vineFieldStep)))
			y1 := min(vineFieldHeight-1, int(math.Ceil((math.Max(a.Y, b.Y)+reach)/vineFieldStep)))
			for y := y0; y <= y1; y++ {
				for x := x0; x <= x1; x++ {
					i := y*vineFieldWidth + x
					if field.clearance[i] == 0 {
						continue
					}
					p := V{(float64(x) + .5) * vineFieldStep, (float64(y) + .5) * vineFieldStep}
					q := a.Add(d.Mul(clamp(p.Sub(a).Dot(d)/length2, 0, 1)))
					distance := p.Sub(q).Len()
					if distance < reach {
						field.clearance[i] = math.Min(field.clearance[i], math.Max(0, distance-foregroundVineGuideClearance))
					}
				}
			}
		}
	}
	return field
}

func generateForegroundVines(foreground RockGrid, guides []Guide, rng *rand.Rand) []Vine {
	vines := generateVinesInBand(newForegroundVineTerrain(foreground, guides), rng, generationWidth, 2*generationWidth, 2)
	for i := range vines {
		vines[i].Foreground = true
	}
	return vines
}

func foregroundVinePalette(vines []Vine, index int) [len(vineColors)]color.NRGBA {
	// Dusty rust and rose retain volume without bright red highlights.
	tints := [...][3]float64{{1, 1, 1}, {.97, .95, 1.04}, {1, 1.05, .96}, {.96, .98, 1.06}}
	tint := tints[vineFamily(vines, index)%len(tints)]
	palette := [len(vineColors)]color.NRGBA{
		{34, 23, 24, 255}, {57, 36, 35, 255}, {82, 53, 47, 255}, {115, 85, 70, 255},
		{98, 65, 54, 255}, {74, 43, 39, 255}, {48, 29, 29, 255}, {29, 20, 22, 255},
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

func prepareForegroundVines(vines []Vine) []triangleMesh {
	if len(vines) == 0 {
		return nil
	}
	stems := prepareVines(vines)
	meshes := make([]triangleMesh, 0, len(stems)*2)
	// A small contact shadow seats the crisp ribbons on the raised surface.
	// Draw all shadows first so they cannot darken a neighboring vine.
	for _, stem := range stems {
		vertices := append([]ebiten.Vertex(nil), stem.vertices...)
		for i := range vertices {
			v := &vertices[i]
			v.DstX += 1
			v.DstY += 1.5
			v.ColorR, v.ColorG, v.ColorB, v.ColorA = 0, 0, 0, .38
		}
		meshes = append(meshes, triangleMesh{vertices: vertices, indices: stem.indices, premultiplied: true})
	}
	return append(meshes, stems...)
}
