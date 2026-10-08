package render

import (
	"math"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

type rockCrevice struct {
	a, b  geom.V
	alpha uint8
}

// Concave changes of slope and exposed depth steps get narrow contact cracks.
// Coplanar and convex joins read through their face tones alone. Intersecting
// collinear edge intervals handles the partial joins of clipped/merged cells.
func rockCrevices(grid terrain.RockGrid, adjacency ...[][]int) []rockCrevice {
	var neighbors [][]int
	if len(adjacency) > 0 {
		neighbors = adjacency[0]
	}
	return rockCrevicesInBand(grid, neighbors, false)
}

func rockCrevicesInBand(grid terrain.RockGrid, neighbors [][]int, owned bool) []rockCrevice {
	if neighbors == nil {
		neighbors = terrain.RockNeighbors(grid)
	}
	var seams []rockCrevice
	for i, neighbors := range neighbors {
		for _, j := range neighbors {
			if j <= i {
				continue
			}
			c, other := grid[i], grid[j]
			if c.Color.A == 0 || other.Color.A == 0 {
				continue
			}
			for k, a := range c.Polygon {
				b := c.Polygon[(k+1)%len(c.Polygon)]
				for l, p := range other.Polygon {
					q := other.Polygon[(l+1)%len(other.Polygon)]
					start, end, ok := sharedRockSegment(a, b, p, q)
					if !ok || (owned && !inTerrainBand(start, end)) {
						continue
					}
					strength := rockCreviceStrength(c, other, start.Add(end).Mul(.5))
					if strength < .08 || end.Sub(start).Len() < .002 {
						continue
					}
					world := terrain.RockMaterialPoint(c, start.Add(end).Mul(.5))
					key := terrain.CollisionVertexKey(world)
					variation := terrain.SiteRandom(0x63726576696365, key[0], key[1], 0)
					// End before junctions so tiny cut tips cannot acquire whiskers.
					d := end.Sub(start)
					inset := math.Min(.0012/d.Len(), .15)
					alpha := uint8(math.Round(100 * strength * (.65 + .35*variation)))
					seams = append(seams, rockCrevice{start.Add(d.Mul(inset)), end.Sub(d.Mul(inset)), alpha})
				}
			}
		}
	}
	return seams
}

func sharedRockSegment(a, b, p, q geom.V) (geom.V, geom.V, bool) {
	d := b.Sub(a)
	length := d.Len()
	if length < 1e-8 || math.Abs(geom.Cross(d, p.Sub(a)))/length > 1e-8 || math.Abs(geom.Cross(d, q.Sub(a)))/length > 1e-8 {
		return geom.V{}, geom.V{}, false
	}
	u, v := p.Sub(a).Dot(d)/d.Len2(), q.Sub(a).Dot(d)/d.Len2()
	lo, hi := math.Max(0, math.Min(u, v)), math.Min(1, math.Max(u, v))
	if (hi-lo)*length < 1e-8 {
		return geom.V{}, geom.V{}, false
	}
	return a.Add(d.Mul(lo)), a.Add(d.Mul(hi)), true
}

func rockCreviceStrength(a, b terrain.RockCell, p geom.V) float64 {
	direction := b.Center.Sub(a.Center).Norm()
	slopeA := (geom.V{X: -a.Normal.X, Y: -a.Normal.Y}).Mul(1 / math.Max(a.Normal.Z, .05))
	slopeB := (geom.V{X: -b.Normal.X, Y: -b.Normal.Y}).Mul(1 / math.Max(b.Normal.Z, .05))
	if !a.Raised && !b.Raised {
		concave := geom.Smoothstep(.025, .25, slopeB.Sub(slopeA).Dot(direction))
		step := geom.Smoothstep(.0005, .003, math.Abs(terrain.RockDepthAt(a, p)-terrain.RockDepthAt(b, p)))
		visible := geom.Smoothstep(2, 16, math.Min(float64(rockViewColor(a, ViewShaded).R), float64(rockViewColor(b, ViewShaded).R)))
		return math.Max(.65*concave, .55*step) * visible
	}
	concave := geom.Smoothstep(.18, .9, slopeB.Sub(slopeA).Dot(direction))
	step := geom.Smoothstep(.003, .016, math.Abs(terrain.RockDepthAt(a, p)-terrain.RockDepthAt(b, p)))
	visible := geom.Smoothstep(12, 65, math.Max(float64(rockViewColor(a, ViewShaded).R), float64(rockViewColor(b, ViewShaded).R)))
	return math.Max(.8*concave, .65*step) * visible
}
