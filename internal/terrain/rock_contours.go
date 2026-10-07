package terrain

import (
	"math"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

const rockContourHeight = .008

// Clip the outer footprint through existing faces instead of exposing a row
// of complete Voronoi tiles. Keep the original face normal and material so
// clipping/triangulation cannot introduce extra wedges of light.
func contourRockGrid(grid RockGrid, heightAt func(geom.V) float64) RockGrid {
	parts := make([]RockGrid, len(grid))
	parallelFor(len(grid), func(n int) {
		c := grid[n]
		for _, poly := range contourRockFace(c.Polygon, c.Center, heightAt) {
			if len(poly) < 3 || geom.PolygonArea(poly) < .000006 {
				continue
			}
			face := c
			face.Polygon = poly
			if !geom.InsidePolygon(face.Center, poly) {
				face.Center = geom.PolygonCenter(poly)
				face.Z = heightAt(face.Center)
			}
			parts[n] = append(parts[n], face)
		}
	})
	var out RockGrid
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

func contourRockFace(poly []geom.V, center geom.V, heightAt func(geom.V) float64) [][]geom.V {
	if len(poly) < 3 {
		return nil
	}
	values := make([]float64, len(poly))
	inside := 0
	for i, p := range poly {
		// On a guide the height is discontinuous. Sample just inside this
		// face so the two sides retain their own boundary elevation.
		q := geom.LerpVector(p, center, 1e-6)
		if !geom.InsidePolygon(q, poly) {
			prev, next := poly[(i+len(poly)-1)%len(poly)], poly[(i+1)%len(poly)]
			q = geom.LerpVector(p, prev.Add(next).Mul(.5), 1e-6)
		}
		values[i] = heightAt(q) - rockContourHeight
		if values[i] >= 0 {
			inside++
		}
	}
	if inside == len(poly) {
		return [][]geom.V{poly}
	}
	if inside == 0 && heightAt(center) <= rockContourHeight {
		return nil
	}
	triangles := geom.Triangulate(poly)
	fan := geom.InsidePolygon(center, poly)
	for i, p := range poly {
		if geom.Cross(poly[(i+1)%len(poly)].Sub(p), center.Sub(p)) < -1e-15 {
			fan = false
		}
	}
	if fan {
		triangles = nil
		for i := range poly {
			triangles = append(triangles, [3]int{len(poly), i, (i + 1) % len(poly)})
		}
		values = append(values, heightAt(center)-rockContourHeight)
		poly = append(append([]geom.V(nil), poly...), center)
	}
	var faces [][]geom.V
	for _, tri := range triangles {
		// Sampling triangle centers preserves narrow spurs passing through
		// a face whose perimeter vertices happen to be outside the footprint.
		mid := poly[tri[0]].Add(poly[tri[1]]).Add(poly[tri[2]]).Mul(1.0 / 3)
		z := heightAt(mid) - rockContourHeight
		for k, a := range tri {
			b := tri[(k+1)%3]
			points := []geom.V{mid, poly[a], poly[b]}
			heights := []float64{z, values[a], values[b]}
			clipped := clipHeightTriangle(points, heights)
			if len(clipped) >= 3 && geom.PolygonArea(clipped) > 1e-13 {
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
func clipHeightTriangle(points []geom.V, values []float64) []geom.V {
	var out []geom.V
	prev, zp := points[len(points)-1], values[len(values)-1]
	for i, p := range points {
		z := values[i]
		if (z >= 0) != (zp >= 0) {
			t := zp / (zp - z)
			out = append(out, geom.LerpVector(prev, p, geom.Clamp(t, 0, 1)))
		}
		if z >= 0 {
			out = append(out, p)
		}
		prev, zp = p, z
	}
	if len(out) < 3 || math.Abs(geom.PolygonArea(out)) < 1e-15 {
		return nil
	}
	return geom.CleanPolygon(out)
}

// Project the sampled outer contour back onto the continuous relief. Linear
// interpolation through a wide facet can otherwise leave small triangular
// teeth. Shared junctions move together, while guide lips stay exactly put.
func polishRockContours(grid RockGrid, guides []Guide, heightAt func(geom.V) float64) {
	type key [2]int64
	keyAt := func(p geom.V) key { return key{int64(math.Round(p.X * 1e7)), int64(math.Round(p.Y * 1e7))} }
	points := make(map[key]geom.V)
	adjacent := make(map[key][]geom.V)
	for _, edge := range ExposedRockEdges(grid) {
		a, b := keyAt(edge.A), keyAt(edge.B)
		points[a], points[b] = edge.A, edge.B
		adjacent[a] = append(adjacent[a], edge.B)
		adjacent[b] = append(adjacent[b], edge.A)
	}
	moves := make(map[key]geom.V)
	keys := make([]key, 0, len(points))
	for k := range points {
		keys = append(keys, k)
	}
	projected := make([]geom.V, len(keys))
	accepted := make([]bool, len(keys))
	parallelFor(len(keys), func(n int) {
		k := keys[n]
		p := points[k]
		if len(adjacent[k]) != 2 {
			return
		}
		_, pr := nearestGuide(p, guides)
		if pr.Dist < .004 || math.Abs(heightAt(p)-rockContourHeight) > .018 {
			return
		}
		q := geom.LerpVector(p, adjacent[k][0].Add(adjacent[k][1]).Mul(.5), .18)
		for iteration := 0; iteration < 6; iteration++ {
			error := heightAt(q) - rockContourHeight
			gradient := geom.V{X: (heightAt(q.Add(geom.V{X: .001, Y: 0})) - heightAt(q.Sub(geom.V{X: .001, Y: 0}))) / .002,
				Y: (heightAt(q.Add(geom.V{X: 0, Y: .001})) - heightAt(q.Sub(geom.V{X: 0, Y: .001}))) / .002}
			if gradient.Len2() < 1e-8 {
				break
			}
			step := gradient.Mul(error / gradient.Len2())
			if step.Len() > .003 {
				step = step.Norm().Mul(.003)
			}
			q = q.Sub(step)
		}
		// A newly sampled world can put a contour near the padding boundary.
		// Reject polishing that would grow geometry outside its generation window.
		if q.X < 0 || q.X > generationWidth || q.Y < GenerationMinY || q.Y > generationMaxY {
			return
		}
		if q.Sub(p).Len() <= .009 && math.Abs(heightAt(q)-rockContourHeight) < .00025 {
			projected[n], accepted[n] = q, true
		}
	})
	for n, k := range keys {
		if accepted[n] {
			moves[k] = projected[n]
		}
	}
	// Reject a move everywhere if it would fold or erase any incident face.
	// Applying the same accepted map to all cells avoids cracks at junctions.
	for {
		rejected := make([][]key, len(grid))
		parallelFor(len(grid), func(n int) {
			c := grid[n]
			poly := append([]geom.V(nil), c.Polygon...)
			changed := false
			for i, p := range poly {
				if q, ok := moves[keyAt(p)]; ok {
					poly[i], changed = q, true
				}
			}
			if changed && (geom.PolygonArea(poly) < .000001 || len(geom.Triangulate(poly)) != len(poly)-2 || !simpleRockOutline(poly)) {
				for _, p := range c.Polygon {
					if _, ok := moves[keyAt(p)]; ok {
						rejected[n] = append(rejected[n], keyAt(p))
					}
				}
			}
		})
		found := false
		for _, list := range rejected {
			for _, k := range list {
				delete(moves, k)
				found = true
			}
		}
		if !found {
			break
		}
	}
	for i := range grid {
		c := &grid[i]
		for j, p := range c.Polygon {
			if q, ok := moves[keyAt(p)]; ok {
				c.Polygon[j] = q
			}
		}
		if !geom.InsidePolygon(c.Center, c.Polygon) {
			c.Center = geom.PolygonCenter(c.Polygon)
			c.Z = heightAt(c.Center)
		}
	}
}

func simpleRockOutline(poly []geom.V) bool {
	for i, a := range poly {
		b := poly[(i+1)%len(poly)]
		for j := i + 2; j < len(poly); j++ {
			if (j+1)%len(poly) == i {
				continue
			}
			c, d := poly[j], poly[(j+1)%len(poly)]
			den := geom.Cross(b.Sub(a), d.Sub(c))
			if math.Abs(den) < 1e-16 {
				continue
			}
			t, u := geom.Cross(c.Sub(a), d.Sub(c))/den, geom.Cross(c.Sub(a), b.Sub(a))/den
			if t > 1e-7 && t < 1-1e-7 && u > 1e-7 && u < 1-1e-7 {
				return false
			}
		}
	}
	return true
}
