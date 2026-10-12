package terrain

import (
	"context"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// Try wider launch angles on both sides, then nearby attachments if the
// first joint is blocked. Successful trials keep the same support and length
// requirements as ordinary growth; no recursive retry tree is created.
func growVineBranch(ctx context.Context, field *VineTerrain, stem Vine, joint, reverseBefore int, radiusScale, reach, phase, side float64, depth int, minimum float64) (Vine, int, float64) {
	best, bestLength, bestJoint, bestSide := Vine{}, 0.0, joint, side
	for _, offset := range [...]int{0, -6, 6} {
		j := joint + offset
		if j < 1 || j >= len(stem.Points)-1 {
			continue
		}
		base := stem.Points[j]
		if base.Radius*radiusScale < .00018 {
			continue
		}
		direction := stem.Points[j+1].P.Sub(stem.Points[j-1].P).Norm()
		if j < reverseBefore {
			direction = direction.Mul(-1)
		}
		for _, angle := range [...]float64{.42, .72, 1.05} {
			for _, sign := range [...]float64{side, -side} {
				if ctx.Err() != nil {
					return Vine{}, joint, side
				}
				v := growVine(field, base.P, geom.Rotate(direction, sign*angle), base.Radius*radiusScale, reach, phase, sign, depth)
				length, _ := v.extent()
				if length > bestLength {
					best, bestLength, bestJoint, bestSide = v, length, j, sign
				}
				if length >= reach*.85 && length >= minimum {
					return best, bestJoint, bestSide
				}
			}
		}
		if bestLength >= minimum {
			return best, bestJoint, bestSide
		}
	}
	return best, bestJoint, bestSide
}
