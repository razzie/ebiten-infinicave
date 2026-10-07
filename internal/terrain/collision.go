package terrain

import (
	"math"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// CollisionGeometry contains the union boundaries of foreground rock in world
// coordinates in scene units. Polygons are closed implicitly and may be concave.
// Outer loops have positive signed area; holes have negative signed area. Physics engines
// requiring convex fixtures must decompose these loops and account for holes.
//
// Geometry includes a section of padding on either side along the scrolling
// axis. Use only the owned square Min/Max when combining adjacent sections.
// Background rock, vines, mushrooms, and decorative bevels are not solid.
type CollisionGeometry struct {
	// ID is 0 at the starting edge, then -1, -2, ... upward or rightward.
	ID  int64
	Top float64
	// Origin and Min/Max describe the owned square in either orientation.
	// Top is legacy vertical metadata and is zero in horizontal scenes.
	Origin, Min, Max geom.V
	Polygons         [][]geom.V
}

// Contains reports whether a world point lies in solid rock, including its
// boundary. It accounts for holes and uses the configured approximation.
func (g CollisionGeometry) Contains(p geom.V) bool {
	winding := 0
	for _, poly := range g.Polygons {
		for i, a := range poly {
			b := poly[(i+1)%len(poly)]
			if geom.SegmentDistanceSquared(p, p, a, b) <= 1e-20 {
				return true
			}
			if a.Y <= p.Y && b.Y > p.Y && geom.Cross(b.Sub(a), p.Sub(a)) > 0 {
				winding++
			} else if a.Y > p.Y && b.Y <= p.Y && geom.Cross(b.Sub(a), p.Sub(a)) < 0 {
				winding--
			}
		}
	}
	return winding != 0
}

func CopyCollisionGeometry(geometry CollisionGeometry) CollisionGeometry {
	if geometry.Polygons != nil {
		geometry.Polygons = geom.CopyPolygons(geometry.Polygons)
	}
	return geometry
}

func CollisionVertexKey(p geom.V) [2]int64 {
	return [2]int64{int64(math.Round(p.X * 1e7)), int64(math.Round(p.Y * 1e7))}
}

// Internal face edges cancel, including partial shared edges. Tracing per
// connected block keeps corner contacts from joining unrelated formations.
func prepareCollisionGeometry(id int64, grid RockGrid, boundary []RockEdge, h *Geometry, tolerance float64) CollisionGeometry {
	geometry := CollisionGeometry{ID: -id, Top: SectionTop(id)}
	geometry.Origin = geom.V{Y: geometry.Top}
	geometry.Min, geometry.Max = geometry.Origin, geometry.Origin.Add(geom.V{X: 1, Y: 1})
	byBlock := make([][]RockEdge, len(h.Blocks))
	for _, edge := range boundary {
		block := h.Faces[edge.Cell].block
		byBlock[block] = append(byBlock[block], edge)
	}
	for block, edges := range byBlock {
		loops, ok := TraceCollisionLoops(edges)
		if !ok {
			// Keep all solid faces if a degenerate junction cannot be traced.
			// Their interiors are disjoint, so Contains still gives the union.
			for _, index := range h.Blocks[block] {
				loops = append(loops, append([]geom.V(nil), grid[index].Polygon...))
			}
		}
		if tolerance > 0 && ok {
			candidates := make([][]geom.V, len(loops))
			for i, loop := range loops {
				candidates[i] = simplifyCollisionLoop(loop, tolerance)
			}
			if compatibleCollisionLoops(loops, candidates) {
				loops = candidates
			}
		}
		for _, loop := range loops {
			world := make([]geom.V, len(loop))
			for i, p := range loop {
				world[i] = p.Add(geom.V{Y: h.Top})
			}
			geometry.Polygons = append(geometry.Polygons, world)
		}
	}
	return geometry
}

// Keep holes inside their parent and avoid crossing boundary loops. If a
// tolerance would break topology, retain exact boundaries for that block.
func compatibleCollisionLoops(original, simplified [][]geom.V) bool {
	for i, a := range simplified {
		for j := i + 1; j < len(simplified); j++ {
			b := simplified[j]
			if geom.InsidePolygon(original[i][0], original[j]) != geom.InsidePolygon(a[0], b) ||
				geom.InsidePolygon(original[j][0], original[i]) != geom.InsidePolygon(b[0], a) {
				return false
			}
			loA, hiA := geom.PolygonBounds(a)
			loB, hiB := geom.PolygonBounds(b)
			if loA.X > hiB.X || loB.X > hiA.X || loA.Y > hiB.Y || loB.Y > hiA.Y {
				continue
			}
			for k, p := range a {
				q := a[(k+1)%len(a)]
				for n, r := range b {
					if geom.SegmentDistanceSquared(p, q, r, b[(n+1)%len(b)]) < 1e-20 {
						return false
					}
				}
			}
		}
	}
	return true
}

func TraceCollisionLoops(edges []RockEdge) ([][]geom.V, bool) {
	outgoing := make(map[[2]int64][]int)
	for i, edge := range edges {
		key := CollisionVertexKey(edge.A)
		outgoing[key] = append(outgoing[key], i)
	}
	used := make([]bool, len(edges))
	var loops [][]geom.V
	for start := range edges {
		if used[start] {
			continue
		}
		first := CollisionVertexKey(edges[start].A)
		var poly []geom.V
		at := start
		for {
			if used[at] {
				return nil, false
			}
			used[at] = true
			edge := edges[at]
			poly = append(poly, edge.A)
			key := CollisionVertexKey(edge.B)
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
				turn := math.Atan2(geom.Cross(incoming, direction), incoming.Dot(direction))
				if turn > angle {
					next, angle = candidate, turn
				}
			}
			if next < 0 {
				return nil, false
			}
			at = next
		}
		if len(poly) < 3 || math.Abs(geom.PolygonArea(poly)) < 1e-15 {
			return nil, false
		}
		loops = append(loops, poly)
	}
	return loops, true
}

// Insert and protect exact section crossings so independently simplified
// generation windows meet at the same points along their owned-band seams.
func collisionSeamVertices(poly []geom.V) []geom.V {
	var out []geom.V
	for i, a := range poly {
		b := poly[(i+1)%len(poly)]
		out = append(out, a)
		var cuts []geom.V
		for _, y := range []float64{GenerationMinY, 0, SectionHeight, generationMaxY} {
			if y > math.Min(a.Y, b.Y)+1e-12 && y < math.Max(a.Y, b.Y)-1e-12 {
				t := (y - a.Y) / (b.Y - a.Y)
				cuts = append(cuts, geom.V{X: geom.Lerp(a.X, b.X, t), Y: y})
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

func simplifyCollisionLoop(original []geom.V, tolerance float64) []geom.V {
	poly := collisionSeamVertices(original)
	anchors := []int{0}
	for i := 1; i < len(poly); i++ {
		p := poly[i]
		if math.Abs(p.Y/SectionHeight-math.Round(p.Y/SectionHeight)) < 1e-12 {
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
	closed := append(append([]geom.V(nil), poly...), poly[0])
	anchors = append(anchors, len(poly))
	for i := 1; i < len(anchors); i++ {
		simplifyCollisionChain(closed, anchors[i-1], anchors[i], tolerance*tolerance, keep)
	}
	var simplified []geom.V
	for i, p := range poly {
		if keep[i] {
			simplified = append(simplified, p)
		}
	}
	if len(simplified) < 3 || len(simplified) >= len(original) || geom.PolygonArea(simplified)*geom.PolygonArea(original) <= 0 || !simpleCollisionLoop(simplified) {
		return original
	}
	return simplified
}

// Iterative Douglas-Peucker keeps the maximum original-vertex deviation within
// tolerance without accumulating errors from successive vertex removals.
func simplifyCollisionChain(poly []geom.V, first, last int, tolerance2 float64, keep []bool) {
	stack := [][2]int{{first, last}}
	for len(stack) > 0 {
		span := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		a, b := span[0], span[1]
		keep[a], keep[b] = true, true
		index, distance := -1, tolerance2
		for i := a + 1; i < b; i++ {
			d2 := geom.SegmentDistanceSquared(poly[i], poly[i], poly[a], poly[b])
			if d2 > distance {
				index, distance = i, d2
			}
		}
		if index >= 0 {
			stack = append(stack, [2]int{a, index}, [2]int{index, b})
		}
	}
}

func simpleCollisionLoop(poly []geom.V) bool {
	for i, a := range poly {
		b := poly[(i+1)%len(poly)]
		for j := i + 2; j < len(poly); j++ {
			if (j+1)%len(poly) == i {
				continue
			}
			if geom.SegmentDistanceSquared(a, b, poly[j], poly[(j+1)%len(poly)]) < 1e-20 {
				return false
			}
		}
	}
	return true
}
