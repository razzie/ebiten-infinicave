package terrain

import (
	"math"
	"math/rand"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

const foregroundVineGuideClearance = .030

func newForegroundVineTerrain(foreground RockGrid, guides []Guide) *VineTerrain {
	return newForegroundVineTerrainWithWorkspace(foreground, guides, nil)
}

func newForegroundVineTerrainWithWorkspace(foreground RockGrid, guides []Guide, workspace *vineWorkspace) *VineTerrain {
	// Growth is based on the visible footprint, including its screen inset,
	// rather than rock brightness: shaded and lit faces both support vines.
	support := InsetForegroundGrid(foreground)
	field := newVineTerrainWithWorkspace(support, nil, true, workspace)
	// Restrict the same clearance field used by root placement, steering,
	// and seam-following twigs so the full family keeps off the crests.
	// The existing conservative sampler also reserves room for ribbon width.
	const reach = foregroundVineGuideClearance + vineBorderRange
	for _, guide := range guides {
		for j := 1; j < len(guide.Pts); j++ {
			a, b := guide.Pts[j-1], guide.Pts[j]
			d := b.Sub(a)
			length2 := d.Len2()
			if length2 < 1e-18 {
				continue
			}
			x0 := max(0, int(math.Floor((min(a.X, b.X)-reach)/vineFieldStep)))
			x1 := min(vineFieldWidth-1, int(math.Ceil((max(a.X, b.X)+reach)/vineFieldStep)))
			y0 := max(0, int(math.Floor((min(a.Y, b.Y)-reach-GenerationMinY)/vineFieldStep)))
			y1 := min(vineFieldHeight-1, int(math.Ceil((max(a.Y, b.Y)+reach-GenerationMinY)/vineFieldStep)))
			for y := y0; y <= y1; y++ {
				for x := x0; x <= x1; x++ {
					i := y*vineFieldWidth + x
					if field.clearance[i] == 0 {
						continue
					}
					p := geom.V{X: (float64(x) + .5) * vineFieldStep, Y: GenerationMinY + (float64(y)+.5)*vineFieldStep}
					q := a.Add(d.Mul(geom.Clamp(p.Sub(a).Dot(d)/length2, 0, 1)))
					distance := p.Sub(q).Len()
					if distance < reach {
						field.clearance[i] = min(field.clearance[i], max(0, distance-foregroundVineGuideClearance))
					}
				}
			}
		}
	}
	return field
}

func generateForegroundVines(foreground RockGrid, guides []Guide, rng *rand.Rand) []Vine {
	return generateForegroundVinesWithWorkspace(foreground, guides, rng, nil)
}

func generateForegroundVinesWithWorkspace(foreground RockGrid, guides []Guide, rng *rand.Rand, workspace *vineWorkspace) []Vine {
	field := newForegroundVineTerrainWithWorkspace(foreground, guides, workspace)
	defer field.release()
	vines := generateVinesInBand(field, rng, 0, SectionHeight, 2)
	for i := range vines {
		vines[i].Foreground = true
	}
	return vines
}
