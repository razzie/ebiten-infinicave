package main

import "math"

const rockContourHeight = 8.0

// Clip the outer footprint through existing faces instead of exposing a row
// of complete Voronoi tiles. Keep the original face normal and material so
// clipping/triangulation cannot introduce extra wedges of light.
func contourRockGrid(grid RockGrid, heightAt func(V) float64) RockGrid {
	var out RockGrid
	for _, c := range grid {
		faces := contourRockFace(c.Polygon, c.Center, heightAt)
		for _, poly := range faces {
			if len(poly) < 3 || faceArea(poly) < 6 {
				continue
			}
			face := c
			face.Polygon = poly
			if !insideFace(face.Center, poly) {
				face.Center = faceCenter(poly)
				face.Z = heightAt(face.Center)
			}
			out = append(out, face)
		}
	}
	return out
}

func contourRockFace(poly []V, center V, heightAt func(V) float64) [][]V {
	if len(poly) < 3 {
		return nil
	}
	values := make([]float64, len(poly))
	inside := 0
	for i, p := range poly {
		// On a guide the height is discontinuous. Sample just inside this
		// face so the two sides retain their own boundary elevation.
		q := lerpV(p, center, 1e-6)
		if !insideFace(q, poly) {
			prev, next := poly[(i+len(poly)-1)%len(poly)], poly[(i+1)%len(poly)]
			q = lerpV(p, prev.Add(next).Mul(.5), 1e-6)
		}
		values[i] = heightAt(q) - rockContourHeight
		if values[i] >= 0 {
			inside++
		}
	}
	if inside == len(poly) {
		return [][]V{poly}
	}
	if inside == 0 && heightAt(center) <= rockContourHeight {
		return nil
	}
	triangles := faceTriangles(poly)
	fan := insideFace(center, poly)
	for i, p := range poly {
		if cross(poly[(i+1)%len(poly)].Sub(p), center.Sub(p)) < -1e-9 {
			fan = false
		}
	}
	if fan {
		triangles = nil
		for i := range poly {
			triangles = append(triangles, [3]int{len(poly), i, (i + 1) % len(poly)})
		}
		values = append(values, heightAt(center)-rockContourHeight)
		poly = append(append([]V(nil), poly...), center)
	}
	var faces [][]V
	for _, tri := range triangles {
		// Sampling triangle centers preserves narrow spurs passing through
		// a face whose perimeter vertices happen to be outside the footprint.
		mid := poly[tri[0]].Add(poly[tri[1]]).Add(poly[tri[2]]).Mul(1.0 / 3)
		z := heightAt(mid) - rockContourHeight
		for k, a := range tri {
			b := tri[(k+1)%3]
			points := []V{mid, poly[a], poly[b]}
			heights := []float64{z, values[a], values[b]}
			clipped := clipHeightTriangle(points, heights)
			if len(clipped) >= 3 && faceArea(clipped) > 1e-7 {
				faces = append(faces, clipped)
			}
		}
	}
	// Remove all internal triangulation edges wherever the result is one
	// simple face. Disconnected islands remain separate opaque polygons.
	for changed := true; changed; {
		changed = false
		for i := 0; i < len(faces); i++ {
			for j := i + 1; j < len(faces); j++ {
				if mergeableBorder(faces[i], faces[j], nil) <= mergeTolerance {
					continue
				}
				if joined := joinFaces(faces[i], faces[j]); len(joined) >= 3 {
					faces[i] = joined
					faces = append(faces[:j], faces[j+1:]...)
					changed = true
					break
				}
			}
		}
	}
	return faces
}

// Height is linear on each sampling triangle; crossings are independent of
// edge traversal direction, so neighboring fragments meet without cracks.
func clipHeightTriangle(points []V, values []float64) []V {
	var out []V
	prev, zp := points[len(points)-1], values[len(values)-1]
	for i, p := range points {
		z := values[i]
		if (z >= 0) != (zp >= 0) {
			t := zp / (zp - z)
			out = append(out, lerpV(prev, p, clamp(t, 0, 1)))
		}
		if z >= 0 {
			out = append(out, p)
		}
		prev, zp = p, z
	}
	if len(out) < 3 || math.Abs(faceArea(out)) < 1e-9 {
		return nil
	}
	return cleanFace(out)
}

// Project the sampled outer contour back onto the continuous relief. Linear
// interpolation through a wide facet can otherwise leave small triangular
// teeth. Shared junctions move together, while guide lips stay exactly put.
func polishRockContours(grid RockGrid, guides []Guide, heightAt func(V) float64) {
	type key [2]int64
	keyAt := func(p V) key { return key{int64(math.Round(p.X * 1e4)), int64(math.Round(p.Y * 1e4))} }
	points := make(map[key]V)
	adjacent := make(map[key][]V)
	for _, edge := range exposedRockEdges(grid) {
		a, b := keyAt(edge.A), keyAt(edge.B)
		points[a], points[b] = edge.A, edge.B
		adjacent[a] = append(adjacent[a], edge.B)
		adjacent[b] = append(adjacent[b], edge.A)
	}
	moves := make(map[key]V)
	for k, p := range points {
		if len(adjacent[k]) != 2 {
			continue
		}
		_, pr := nearestGuide(p, guides)
		if pr.Dist < 4 || math.Abs(heightAt(p)-rockContourHeight) > 18 {
			continue
		}
		q := lerpV(p, adjacent[k][0].Add(adjacent[k][1]).Mul(.5), .18)
		for iteration := 0; iteration < 6; iteration++ {
			error := heightAt(q) - rockContourHeight
			gradient := V{(heightAt(q.Add(V{1, 0})) - heightAt(q.Sub(V{1, 0}))) * .5,
				(heightAt(q.Add(V{0, 1})) - heightAt(q.Sub(V{0, 1}))) * .5}
			if gradient.Len2() < 1e-8 {
				break
			}
			step := gradient.Mul(error / gradient.Len2())
			if step.Len() > 3 {
				step = step.Norm().Mul(3)
			}
			q = q.Sub(step)
		}
		if q.Sub(p).Len() <= 9 && math.Abs(heightAt(q)-rockContourHeight) < .25 {
			moves[k] = q
		}
	}
	// Reject a move everywhere if it would fold or erase any incident face.
	// Applying the same accepted map to all cells avoids cracks at junctions.
	for {
		reject := make(map[key]bool)
		for _, c := range grid {
			poly := append([]V(nil), c.Polygon...)
			changed := false
			for i, p := range poly {
				if q, ok := moves[keyAt(p)]; ok {
					poly[i], changed = q, true
				}
			}
			if changed && (faceArea(poly) < 1 || len(faceTriangles(poly)) != len(poly)-2 || !simpleRockOutline(poly)) {
				for _, p := range c.Polygon {
					if _, ok := moves[keyAt(p)]; ok {
						reject[keyAt(p)] = true
					}
				}
			}
		}
		if len(reject) == 0 {
			break
		}
		for k := range reject {
			delete(moves, k)
		}
	}
	for i := range grid {
		c := &grid[i]
		for j, p := range c.Polygon {
			if q, ok := moves[keyAt(p)]; ok {
				c.Polygon[j] = q
			}
		}
		if !insideFace(c.Center, c.Polygon) {
			c.Center = faceCenter(c.Polygon)
			c.Z = heightAt(c.Center)
		}
	}
}

func simpleRockOutline(poly []V) bool {
	for i, a := range poly {
		b := poly[(i+1)%len(poly)]
		for j := i + 2; j < len(poly); j++ {
			if (j+1)%len(poly) == i {
				continue
			}
			c, d := poly[j], poly[(j+1)%len(poly)]
			den := cross(b.Sub(a), d.Sub(c))
			if math.Abs(den) < 1e-10 {
				continue
			}
			t, u := cross(c.Sub(a), d.Sub(c))/den, cross(c.Sub(a), b.Sub(a))/den
			if t > 1e-7 && t < 1-1e-7 && u > 1e-7 && u < 1-1e-7 {
				return false
			}
		}
	}
	return true
}
