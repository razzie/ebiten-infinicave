package render

import (
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

type growthClearance struct {
	grid  terrain.RockGrid
	index *geom.BoundsIndex
}

// Reuse one small spatial index for all hanging samples in a section.
func newGrowthClearance(grid terrain.RockGrid) growthClearance {
	c := growthClearance{grid: grid}
	bounds := make([]geom.Bounds, len(grid))
	for i, cell := range grid {
		lo, hi := geom.PolygonBounds(cell.Polygon)
		bounds[i] = geom.Bounds{Min: lo, Max: hi}
	}
	c.index = geom.NewBoundsIndex(bounds)
	return c
}

func (c growthClearance) fits(a Attachment) bool {
	recipe := AtlasRecipes(a.Mesh.Texture)[a.Cell]
	for i := 0; i < len(recipe.Indices); i += 3 {
		var tri [3]geom.V
		nearRoot := true
		for j, id := range recipe.Indices[i : i+3] {
			v := recipe.Vertices[id]
			p := geom.V{X: float64(v.DstX) / RasterPixelsPerUnit, Y: float64(v.DstY)/RasterPixelsPerUnit + terrain.GenerationMinY}
			nearRoot = nearRoot && p.Len() < .002
			tri[j] = a.Anchor.Add(terrain.InternalPoint(a.Orientation, geom.Rotate(p, a.Rotation).Mul(a.Scale)))
		}
		if nearRoot {
			continue
		}
		if geom.PolygonArea(tri[:]) < 0 {
			tri[0], tri[2] = tri[2], tri[0]
		}
		lo, hi := geom.PolygonBounds(tri[:])
		clear := true
		c.index.Query(geom.Bounds{Min: lo, Max: hi}, func(j int) bool {
			if c.grid[j].Color.A == 0 {
				return true
			}
			clear = !growthTriangleOverlap(tri[:], c.grid[j].Polygon)
			return clear
		})
		if !clear {
			return false
		}
	}
	return true
}

// An indexed narrow phase handles concave faces directly without retaining a
// triangulation of every unrelated rock cell.
func growthTriangleOverlap(tri, face []geom.V) bool {
	for _, p := range tri {
		if geom.InsidePolygon(p, face) {
			return true
		}
	}
	for _, p := range face {
		if geom.InsidePolygon(p, tri) {
			return true
		}
	}
	for i, a := range tri {
		for j, b := range face {
			if geom.SegmentDistanceSquared(a, tri[(i+1)%3], b, face[(j+1)%len(face)]) < 1e-20 {
				return true
			}
		}
	}
	return false
}
