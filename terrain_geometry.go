package infinicave

import "math"

type terrainFace struct {
	poly     []V
	min, max V
	block    int
	key      [2]int64 // stable world-space interior point
}

// Retain the same clipped faces used for rendering and derive collision
// boundaries from their union for collisions, queries, and terrain edits.
type terrainGeometry struct {
	grid       RockGrid      // final inset faces, retained for terrain edits
	topology   *rockTopology // immutable adjacency and boundaries shared with rendering
	cuts       []rockCut
	vegetation vegetationGeometry
	faces      []terrainFace
	guides     []Guide
	blocks     [][]int
	top        float64
	collision  CollisionGeometry
}

func polygonBounds(poly []V) (lo, hi V) {
	lo, hi = poly[0], poly[0]
	for _, p := range poly[1:] {
		lo = V{math.Min(lo.X, p.X), math.Min(lo.Y, p.Y)}
		hi = V{math.Max(hi.X, p.X), math.Max(hi.Y, p.Y)}
	}
	return
}

func prepareTerrainGeometry(data sectionData, tolerance float64) *terrainGeometry {
	h := &terrainGeometry{guides: data.guides, top: sectionTop(data.id), vegetation: sectionVegetation(data)}
	var grid RockGrid
	source := data.foregroundTopology
	if source == nil {
		source = newRockTopology(data.foreground)
	}
	h.cuts = source.cuts
	for _, cell := range source.grid {
		if !cell.Raised || cell.Color.A == 0 {
			continue
		}
		lo, hi := polygonBounds(cell.Polygon)
		center := faceCenter(cell.Polygon)
		key := [2]int64{int64(math.Round(center.X * 1e6)), int64(math.Round((center.Y + sectionTop(data.id)) * 1e6))}
		h.faces = append(h.faces, terrainFace{poly: cell.Polygon, min: lo, max: hi, block: -1, key: key})
		grid = append(grid, cell)
	}
	// Shared edges connect an entire formation, including partial borders
	// left by guide cuts. A corner contact alone keeps two blocks separate.
	neighbors := source.neighbors
	if len(grid) != len(source.grid) {
		neighbors = rockNeighbors(grid)
		source = &rockTopology{grid: grid, neighbors: neighbors, cuts: source.cuts, top: source.top}
		source.prepareBoundary()
	}
	h.grid = grid
	h.topology = source
	for i := range h.faces {
		if h.faces[i].block >= 0 {
			continue
		}
		block := len(h.blocks)
		queue := []int{i}
		h.faces[i].block = block
		for next := 0; next < len(queue); next++ {
			for _, j := range neighbors[queue[next]] {
				if h.faces[j].block < 0 {
					h.faces[j].block = block
					queue = append(queue, j)
				}
			}
		}
		h.blocks = append(h.blocks, queue)
	}
	h.collision = prepareCollisionGeometry(data.id, grid, source.allBoundary, h, tolerance)
	return h
}
