package terrain

import (
	"math"
	"math/rand"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// Polygon follows the attachment, left shoulder, pointed tip, right shoulder.
// Along associates the leaf with its original stem through repeated cuts.
type VineLeaf struct {
	Anchor  geom.V
	Along   float64
	Polygon [4]geom.V
}

func generateVineLeaves(field *VineTerrain, vine Vine, rng *rand.Rand) []VineLeaf {
	if vine.EdgeAligned || len(vine.Points) < 3 {
		return nil
	}
	var leaves []VineLeaf
	next := vine.Along[0] + geom.Lerp(.025, .055, rng.Float64())
	side := 1.0
	if rng.Intn(2) == 0 {
		side = -1
	}
	for i := 1; i < len(vine.Points)-1; i++ {
		if vine.Along[i] < next {
			continue
		}
		next = vine.Along[i] + geom.Lerp(.045, .085, rng.Float64())
		side = -side
		p := vine.Points[i]
		green := vine.Style.Green(vine.Along[i])
		if p.Radius < .0007 || green < .5 || rng.Float64() > green*.85 {
			continue
		}
		tangent := vine.Points[i+1].P.Sub(vine.Points[i-1].P).Norm()
		length := geom.Clamp(p.Radius*geom.Lerp(6, 9, rng.Float64()), .009, .025)
		// Angular, unequal halves echo the faceted rock and reference leaves.
		direction := geom.Rotate(tangent, side*geom.Lerp(.65, 1.25, rng.Float64()))
		base := p.P.Add(tangent.Perp().Mul(side * p.Radius * .7))
		tip := base.Add(direction.Mul(length))
		shoulder := base.Add(direction.Mul(length * .45))
		normal := direction.Perp().Mul(length * .24)
		leaf := VineLeaf{Anchor: p.P, Along: vine.Along[i], Polygon: [4]geom.V{base, shoulder.Add(normal), tip, shoulder.Sub(normal.Mul(.75))}}
		if vineLeafFits(field, leaf) {
			leaves = append(leaves, leaf)
		}
	}
	return leaves
}

func vineLeafFits(field *VineTerrain, leaf VineLeaf) bool {
	// Sample the interior as well as edges: a narrow gap or crest exclusion
	// can pass through a leaf even if all four corners lie on supporting rock.
	for _, triangle := range [][3]geom.V{
		{leaf.Polygon[0], leaf.Polygon[1], leaf.Polygon[2]},
		{leaf.Polygon[0], leaf.Polygon[2], leaf.Polygon[3]},
	} {
		steps := max(1, int(math.Ceil(max(triangle[1].Sub(triangle[0]).Len(), triangle[2].Sub(triangle[0]).Len())/(vineFieldStep*.5))))
		for i := 0; i <= steps; i++ {
			for j := 0; j <= steps-i; j++ {
				p := triangle[0].Add(triangle[1].Sub(triangle[0]).Mul(float64(i) / float64(steps))).Add(triangle[2].Sub(triangle[0]).Mul(float64(j) / float64(steps)))
				if field.growthSpace(p) < .0015 {
					return false
				}
			}
		}
	}
	return true
}
