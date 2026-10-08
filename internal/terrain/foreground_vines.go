package terrain

import (
	"context"
	"math"
	"math/rand"
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
				if x0 <= x1 {
					segmentDistanceSpan(field.clearance[y*vineFieldWidth+x0:y*vineFieldWidth+x1+1], x0, GenerationMinY+(float64(y)+.5)*vineFieldStep, a, d, length2, true, true)
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
	return generateForegroundVinesContext(context.Background(), foreground, guides, rng, workspace)
}

func generateForegroundVinesContext(ctx context.Context, foreground RockGrid, guides []Guide, rng *rand.Rand, workspace *vineWorkspace) []Vine {
	field := newForegroundVineTerrainWithWorkspace(foreground, guides, workspace)
	defer field.release()
	vines := generateVinesInBandContext(ctx, field, rng, 0, SectionHeight, 2)
	for i := range vines {
		vines[i].Foreground = true
	}
	return vines
}
