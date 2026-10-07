package terrain

import (
	"image/color"
	"math"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func cellColor(v float64) color.NRGBA {
	// Sampled rock colors stay charcoal, with restrained warm-gray highlights.
	stops := []struct {
		value   float64
		r, g, b float64
	}{
		{0, 0, 0, 0},
		{.10, 8, 8, 7},
		{.25, 18, 18, 16},
		{.43, 38, 37, 34},
		{.65, 120, 116, 106},
		{.85, 165, 158, 142},
		{1, 190, 182, 164},
	}
	v = geom.Clamp(v, 0, 1)
	for i := 1; i < len(stops); i++ {
		a, b := stops[i-1], stops[i]
		if v <= b.value {
			t := (v - a.value) / (b.value - a.value)
			return color.NRGBA{uint8(math.Round(geom.Lerp(a.r, b.r, t))), uint8(math.Round(geom.Lerp(a.g, b.g, t))), uint8(math.Round(geom.Lerp(a.b, b.b, t))), 255}
		}
	}
	return color.NRGBA{221, 215, 200, 255}
}

// The underlying tessellation has no guide influence. Low Perlin values map
// to exact black; the remaining cells stay within the charcoal palette.
func backgroundCellColor(p geom.V, noise *Perlin) color.NRGBA {
	return backgroundSurfaceColor(p, noise, geom.V3{Z: 1})
}

func backgroundSurfaceColor(p geom.V, noise *Perlin, normal geom.V3, orientation ...Orientation) color.NRGBA {
	return cellColor(.38 * geom.Smoothstep(.43, .59, fbm(noise, p)) * (.28 + .72*SurfaceLight(normal, orientation...)))
}

// Rock occupancy follows relief, independently of light. Every existing face
// is opaque, including its dark flank and the tapered ends of raised spurs.
func guideCellColor(p geom.V, guides []Guide, noise *Perlin, branches *BranchField) color.NRGBA {
	dx := reliefHeight(p.Add(geom.V{X: .002, Y: 0}), guides, noise, branches) - reliefHeight(p.Sub(geom.V{X: .002, Y: 0}), guides, noise, branches)
	dy := reliefHeight(p.Add(geom.V{X: 0, Y: .002}), guides, noise, branches) - reliefHeight(p.Sub(geom.V{X: 0, Y: .002}), guides, noise, branches)
	return guideSurfaceColor(p, guides, noise, branches, (geom.V3{X: -dx / .004, Y: -dy / .004, Z: 1}).Norm())
}

func guideSurfaceColor(p geom.V, guides []Guide, noise *Perlin, branches *BranchField, normal geom.V3) color.NRGBA {
	if reliefHeight(p, guides, noise, branches) <= rockContourHeight {
		return color.NRGBA{}
	}
	return RockSurfaceColor(normal, 1, 1)
}
