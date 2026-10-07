package terrain

import (
	"math"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

type Face struct {
	Poly     []geom.V
	Min, Max geom.V
	block    int
	Key      [2]int64 // stable world-space interior point
}

// Retain the same clipped faces used for rendering and derive collision
// boundaries from their union for collisions, queries, and terrain edits.
type Geometry struct {
	Grid       RockGrid      // final inset faces, retained for terrain edits
	Topology   *RockTopology // immutable adjacency and boundaries shared with rendering
	Cuts       []RockCut
	Vegetation Vegetation
	Faces      []Face
	Guides     []Guide
	Blocks     [][]int
	Top        float64
	Collision  CollisionGeometry
}

func PrepareTerrainGeometry(data SectionData, tolerance float64) *Geometry {
	h := &Geometry{Guides: data.Guides, Top: SectionTop(data.ID), Vegetation: SectionVegetation(data)}
	var grid RockGrid
	source := data.ForegroundTopology
	if source == nil {
		source = newRockTopology(data.Foreground)
	}
	h.Cuts = source.Cuts
	for _, cell := range source.Grid {
		if !cell.Raised || cell.Color.A == 0 {
			continue
		}
		lo, hi := geom.PolygonBounds(cell.Polygon)
		center := geom.PolygonCenter(cell.Polygon)
		key := [2]int64{int64(math.Round(center.X * 1e6)), int64(math.Round((center.Y + SectionTop(data.ID)) * 1e6))}
		h.Faces = append(h.Faces, Face{Poly: cell.Polygon, Min: lo, Max: hi, block: -1, Key: key})
		grid = append(grid, cell)
	}
	// Shared edges connect an entire formation, including partial borders
	// left by guide cuts. A corner contact alone keeps two blocks separate.
	neighbors := source.neighbors
	if len(grid) != len(source.Grid) {
		neighbors = RockNeighbors(grid)
		source = &RockTopology{Grid: grid, neighbors: neighbors, Cuts: source.Cuts, Top: source.Top}
		source.prepareBoundary()
	}
	h.Grid = grid
	h.Topology = source
	for i := range h.Faces {
		if h.Faces[i].block >= 0 {
			continue
		}
		block := len(h.Blocks)
		queue := []int{i}
		h.Faces[i].block = block
		for next := 0; next < len(queue); next++ {
			for _, j := range neighbors[queue[next]] {
				if h.Faces[j].block < 0 {
					h.Faces[j].block = block
					queue = append(queue, j)
				}
			}
		}
		h.Blocks = append(h.Blocks, queue)
	}
	h.Collision = prepareCollisionGeometry(data.ID, grid, source.allBoundary, h, tolerance)
	return h
}
