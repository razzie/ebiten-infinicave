package infinicave

import "math"

// CollisionGeometry contains the union boundaries of foreground rock in world
// coordinates in scene units. Polygons are closed implicitly and may be concave.
// Outer loops have positive signed area; holes have negative signed area. Physics engines
// requiring convex fixtures must decompose these loops and account for holes.
//
// Geometry includes a section of padding on either side of the owned band,
// [Top, Top+SectionHeight). Use only that band when combining adjacent sections.
// Background rock, vines, mushrooms, and decorative bevels are not solid.
type CollisionGeometry struct {
	// ID is 0 at the floor, then -1, -2, ... upward.
	ID       int64
	Top      float64
	Polygons [][]V
}

// Contains reports whether a world point lies in solid rock, including its
// boundary. It accounts for holes and uses the configured approximation.
func (g CollisionGeometry) Contains(p V) bool {
	winding := 0
	for _, poly := range g.Polygons {
		for i, a := range poly {
			b := poly[(i+1)%len(poly)]
			if guideSegmentsDistance2(p, p, a, b) <= 1e-20 {
				return true
			}
			if a.Y <= p.Y && b.Y > p.Y && cross(b.Sub(a), p.Sub(a)) > 0 {
				winding++
			} else if a.Y > p.Y && b.Y <= p.Y && cross(b.Sub(a), p.Sub(a)) < 0 {
				winding--
			}
		}
	}
	return winding != 0
}

// CollisionGeometry returns a copy of a cached section's collision boundaries.
// Geometry becomes available before vegetation generation and render uploads.
// Missing, evicted, and closed sections return false.
// IDs are 0 at the floor, then -1, -2, ... upward; positive IDs return false.
// Call after Update; retain the returned copy as long as your game needs it.
func (g *Scene) CollisionGeometry(id int64) (CollisionGeometry, bool) {
	if g.closed || id > 0 || id == -1<<63 {
		return CollisionGeometry{}, false
	}
	section := g.world.sections[-id]
	if section != nil && section.geometry != nil {
		return copyCollisionGeometry(section.geometry.collision), true
	}
	if geometry := g.world.collision[-id]; geometry != nil {
		return copyCollisionGeometry(geometry.collision), true
	}
	return CollisionGeometry{}, false
}

func copyCollisionGeometry(geometry CollisionGeometry) CollisionGeometry {
	if geometry.Polygons != nil {
		geometry.Polygons = copyQueryPolygons(geometry.Polygons)
	}
	return geometry
}

// Drain early terrain independently of mesh reception and GPU upload budgets.
// The generation worker never invokes application code on its goroutine.
func (w *world) receiveCollision(g *Scene) {
	select {
	case terrain := <-w.terrain:
		geometry := terrain.geometry
		for _, cut := range w.cuts {
			geometry, _, _ = cut.geometry(terrain.id, geometry, g.collisionTolerance)
		}
		if w.collision == nil {
			w.collision = make(map[int64]*terrainGeometry)
		}
		w.collision[terrain.id] = geometry
		if g.onCollisionReady != nil {
			g.onCollisionReady(copyCollisionGeometry(geometry.collision))
		}
	default:
	}
}

func collisionVertexKey(p V) [2]int64 {
	return [2]int64{int64(math.Round(p.X * 1e7)), int64(math.Round(p.Y * 1e7))}
}

// Internal face edges cancel, including partial shared edges. Tracing per
// connected block keeps corner contacts from joining unrelated formations.
func prepareCollisionGeometry(id int64, grid RockGrid, neighbors [][]int, h *terrainGeometry, tolerance float64) CollisionGeometry {
	geometry := CollisionGeometry{ID: -id, Top: sectionTop(id)}
	byBlock := make([][]rockEdge, len(h.blocks))
	for _, edge := range rockBoundaryEdges(grid, neighbors, true) {
		block := h.faces[edge.Cell].block
		byBlock[block] = append(byBlock[block], edge)
	}
	for block, edges := range byBlock {
		loops, ok := traceCollisionLoops(edges)
		if !ok {
			// Keep all solid faces if a degenerate junction cannot be traced.
			// Their interiors are disjoint, so Contains still gives the union.
			for _, index := range h.blocks[block] {
				loops = append(loops, append([]V(nil), grid[index].Polygon...))
			}
		}
		if tolerance > 0 && ok {
			candidates := make([][]V, len(loops))
			for i, loop := range loops {
				candidates[i] = simplifyCollisionLoop(loop, tolerance)
			}
			if compatibleCollisionLoops(loops, candidates) {
				loops = candidates
			}
		}
		for _, loop := range loops {
			world := make([]V, len(loop))
			for i, p := range loop {
				world[i] = p.Add(V{Y: h.top})
			}
			geometry.Polygons = append(geometry.Polygons, world)
		}
	}
	return geometry
}

// Keep holes inside their parent and avoid crossing boundary loops. If a
// tolerance would break topology, retain exact boundaries for that block.
func compatibleCollisionLoops(original, simplified [][]V) bool {
	for i, a := range simplified {
		for j := i + 1; j < len(simplified); j++ {
			b := simplified[j]
			if insideFace(original[i][0], original[j]) != insideFace(a[0], b) ||
				insideFace(original[j][0], original[i]) != insideFace(b[0], a) {
				return false
			}
			loA, hiA := polygonBounds(a)
			loB, hiB := polygonBounds(b)
			if loA.X > hiB.X || loB.X > hiA.X || loA.Y > hiB.Y || loB.Y > hiA.Y {
				continue
			}
			for k, p := range a {
				q := a[(k+1)%len(a)]
				for n, r := range b {
					if guideSegmentsDistance2(p, q, r, b[(n+1)%len(b)]) < 1e-20 {
						return false
					}
				}
			}
		}
	}
	return true
}

func traceCollisionLoops(edges []rockEdge) ([][]V, bool) {
	outgoing := make(map[[2]int64][]int)
	for i, edge := range edges {
		key := collisionVertexKey(edge.A)
		outgoing[key] = append(outgoing[key], i)
	}
	used := make([]bool, len(edges))
	var loops [][]V
	for start := range edges {
		if used[start] {
			continue
		}
		first := collisionVertexKey(edges[start].A)
		var poly []V
		at := start
		for {
			if used[at] {
				return nil, false
			}
			used[at] = true
			edge := edges[at]
			poly = append(poly, edge.A)
			key := collisionVertexKey(edge.B)
			if key == first {
				break
			}
			next, angle := -1, math.Inf(-1)
			incoming := edge.B.Sub(edge.A)
			for _, candidate := range outgoing[key] {
				if used[candidate] {
					continue
				}
				direction := edges[candidate].B.Sub(edges[candidate].A)
				turn := math.Atan2(cross(incoming, direction), incoming.Dot(direction))
				if turn > angle {
					next, angle = candidate, turn
				}
			}
			if next < 0 {
				return nil, false
			}
			at = next
		}
		if len(poly) < 3 || math.Abs(faceArea(poly)) < 1e-15 {
			return nil, false
		}
		loops = append(loops, poly)
	}
	return loops, true
}

// Insert and protect exact section crossings so independently simplified
// generation windows meet at the same points along their owned-band seams.
func collisionSeamVertices(poly []V) []V {
	var out []V
	for i, a := range poly {
		b := poly[(i+1)%len(poly)]
		out = append(out, a)
		var cuts []V
		for _, y := range []float64{generationMinY, 0, SectionHeight, generationMaxY} {
			if y > math.Min(a.Y, b.Y)+1e-12 && y < math.Max(a.Y, b.Y)-1e-12 {
				t := (y - a.Y) / (b.Y - a.Y)
				cuts = append(cuts, V{lerp(a.X, b.X, t), y})
			}
		}
		if b.Y < a.Y {
			for j := len(cuts) - 1; j >= 0; j-- {
				out = append(out, cuts[j])
			}
		} else {
			out = append(out, cuts...)
		}
	}
	return out
}

func simplifyCollisionLoop(original []V, tolerance float64) []V {
	poly := collisionSeamVertices(original)
	anchors := []int{0}
	for i := 1; i < len(poly); i++ {
		p := poly[i]
		if math.Abs(p.Y/sectionHeight-math.Round(p.Y/sectionHeight)) < 1e-12 {
			anchors = append(anchors, i)
		}
	}
	if len(anchors) == 1 {
		farthest := 1
		for i := 2; i < len(poly); i++ {
			if poly[i].Sub(poly[0]).Len2() > poly[farthest].Sub(poly[0]).Len2() {
				farthest = i
			}
		}
		anchors = append(anchors, farthest)
	}
	keep := make([]bool, len(poly)+1)
	closed := append(append([]V(nil), poly...), poly[0])
	anchors = append(anchors, len(poly))
	for i := 1; i < len(anchors); i++ {
		simplifyCollisionChain(closed, anchors[i-1], anchors[i], tolerance*tolerance, keep)
	}
	var simplified []V
	for i, p := range poly {
		if keep[i] {
			simplified = append(simplified, p)
		}
	}
	if len(simplified) < 3 || len(simplified) >= len(original) || faceArea(simplified)*faceArea(original) <= 0 || !simpleCollisionLoop(simplified) {
		return original
	}
	return simplified
}

// Iterative Douglas-Peucker keeps the maximum original-vertex deviation within
// tolerance without accumulating errors from successive vertex removals.
func simplifyCollisionChain(poly []V, first, last int, tolerance2 float64, keep []bool) {
	stack := [][2]int{{first, last}}
	for len(stack) > 0 {
		span := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		a, b := span[0], span[1]
		keep[a], keep[b] = true, true
		index, distance := -1, tolerance2
		for i := a + 1; i < b; i++ {
			d2 := guideSegmentsDistance2(poly[i], poly[i], poly[a], poly[b])
			if d2 > distance {
				index, distance = i, d2
			}
		}
		if index >= 0 {
			stack = append(stack, [2]int{a, index}, [2]int{index, b})
		}
	}
}

func simpleCollisionLoop(poly []V) bool {
	for i, a := range poly {
		b := poly[(i+1)%len(poly)]
		for j := i + 2; j < len(poly); j++ {
			if (j+1)%len(poly) == i {
				continue
			}
			if guideSegmentsDistance2(a, b, poly[j], poly[(j+1)%len(poly)]) < 1e-20 {
				return false
			}
		}
	}
	return true
}
