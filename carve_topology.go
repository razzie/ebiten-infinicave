package infinicave

import "sort"

// Carving only removes area: fragments can touch siblings or the original
// face's neighbors, but cannot acquire a new neighbor elsewhere in the grid.
// Keep unchanged adjacency and boundaries instead of rebuilding the section.
func carveTopology(old *rockTopology, grid RockGrid, parents []int, cuts []rockCut, top float64) *rockTopology {
	children := make([][]int, len(old.grid))
	for i, parent := range parents {
		children[parent] = append(children[parent], i)
	}
	changed := make([]bool, len(old.grid))
	for i, faces := range children {
		changed[i] = len(faces) != 1 || len(grid[faces[0]].Polygon) != len(old.grid[i].Polygon) ||
			&grid[faces[0]].Polygon[0] != &old.grid[i].Polygon[0]
	}
	t := &rockTopology{grid: grid, neighbors: make([][]int, len(grid)),
		allBoundary: make([]rockEdge, 0, len(old.allBoundary)), cuts: cuts, top: top}
	join := func(a, b int, unchanged bool) {
		if unchanged || mergeableBorder(grid[a].Polygon, grid[b].Polygon, nil) > mergeTolerance {
			t.neighbors[a] = append(t.neighbors[a], b)
			t.neighbors[b] = append(t.neighbors[b], a)
		}
	}
	dirty := make([]bool, len(old.grid))
	for i, faces := range children {
		if changed[i] {
			dirty[i] = true
			for _, j := range old.neighbors[i] {
				dirty[j] = true
			}
			for a, face := range faces {
				for _, other := range faces[a+1:] {
					join(face, other, false)
				}
			}
		}
		for _, j := range old.neighbors[i] {
			if j <= i {
				continue
			}
			for _, face := range faces {
				for _, other := range children[j] {
					join(face, other, !changed[i] && !changed[j])
				}
			}
		}
	}
	for _, neighbors := range t.neighbors {
		sort.Ints(neighbors)
	}

	// Include a halo so cancellation sees both sides of every dirty edge.
	// Only edges owned by dirty faces replace the cached boundary.
	included := make([]bool, len(grid))
	for i, parent := range parents {
		if dirty[parent] {
			included[i] = true
			for _, j := range t.neighbors[i] {
				included[j] = true
			}
		}
	}
	var localGrid RockGrid
	var globalIDs []int
	localIDs := make([]int, len(grid))
	for i, include := range included {
		if include {
			localIDs[i] = len(localGrid)
			localGrid = append(localGrid, grid[i])
			globalIDs = append(globalIDs, i)
		}
	}
	localNeighbors := make([][]int, len(localGrid))
	for i, global := range globalIDs {
		for _, neighbor := range t.neighbors[global] {
			if included[neighbor] {
				localNeighbors[i] = append(localNeighbors[i], localIDs[neighbor])
			}
		}
	}
	for _, edge := range old.allBoundary {
		if !dirty[edge.Cell] {
			edge.Cell = children[edge.Cell][0]
			t.allBoundary = append(t.allBoundary, edge)
		}
	}
	localBoundary := rockBoundaryEdges(localGrid, localNeighbors, true)
	replacements := localBoundary[:0]
	for _, edge := range localBoundary {
		edge.Cell = globalIDs[edge.Cell]
		if dirty[parents[edge.Cell]] {
			replacements = append(replacements, edge)
		}
	}
	// Both lists retain full-rebuild ordering. Merge them in linear time;
	// sorting the whole boundary would dominate small edits in large sections.
	cached := t.allBoundary
	t.allBoundary = make([]rockEdge, 0, len(cached)+len(replacements))
	i, j := 0, 0
	for i < len(cached) && j < len(replacements) {
		a, b := edgeKey(cached[i].A, cached[i].B), edgeKey(replacements[j].A, replacements[j].B)
		less := false
		for axis := range a {
			if a[axis] != b[axis] {
				less = a[axis] < b[axis]
				break
			}
		}
		if less {
			t.allBoundary = append(t.allBoundary, cached[i])
			i++
		} else {
			t.allBoundary = append(t.allBoundary, replacements[j])
			j++
		}
	}
	t.allBoundary = append(t.allBoundary, cached[i:]...)
	t.allBoundary = append(t.allBoundary, replacements[j:]...)
	for _, edge := range t.allBoundary {
		if !rockWindowEdge(edge.A, edge.B) {
			t.boundary = append(t.boundary, edge)
		}
	}
	return t
}
