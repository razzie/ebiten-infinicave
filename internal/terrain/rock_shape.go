package terrain

import (
	"math"
	"math/rand"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

const rockContourHeight = .008

// Widen the shared formation footprint by several cells so neighboring rear
// formations overlap, while leaving a visible band around the foreground.
const backgroundRockPadding = .085

const backgroundBorderWidth = .14
const backgroundSolidBorderWidth = backgroundBorderWidth - .025 - .012

// rockFootprint measures the original, uncut formations. Reusing their actual
// contours preserves every guide and branch without growing a second skeleton.
type rockFootprint struct {
	grid            RockGrid
	faces, boundary *geom.BoundsIndex
	edges           []RockEdge
	worldTop        float64
	minX, maxX      float64
	background      *rockFootprint // Actual rear cells, including gaps between formations.
}

func newRockFootprint(grid RockGrid) *rockFootprint {
	f := &rockFootprint{grid: grid, edges: ExposedRockEdges(grid), minX: math.Inf(1), maxX: math.Inf(-1)}
	if len(grid) > 0 {
		f.worldTop = grid[0].worldTop
	}
	bounds := make([]geom.Bounds, len(grid))
	for i, c := range grid {
		lo, hi := geom.PolygonBounds(c.Polygon)
		bounds[i] = geom.Bounds{Min: lo, Max: hi}
		f.minX, f.maxX = min(f.minX, lo.X), max(f.maxX, hi.X)
	}
	f.faces = geom.NewBoundsIndex(bounds)
	bounds = make([]geom.Bounds, len(f.edges))
	for i, e := range f.edges {
		bounds[i] = geom.Bounds{Min: geom.V{X: min(e.A.X, e.B.X), Y: min(e.A.Y, e.B.Y)}, Max: geom.V{X: max(e.A.X, e.B.X), Y: max(e.A.Y, e.B.Y)}}
	}
	f.boundary = geom.NewBoundsIndex(bounds)
	return f
}

// Negative distances are inside rock. With no foreground, infinity omits
// formation padding while the independent background border strips remain.
func (f *rockFootprint) distance(p geom.V) float64 {
	d2 := f.boundary.Nearest(p, math.Inf(1), func(i int, best float64) float64 {
		e := f.edges[i]
		return min(best, geom.SegmentDistanceSquared(p, p, e.A, e.B))
	})
	d := math.Sqrt(d2)
	if f.contains(p) {
		d = -d
	}
	return d
}

func (f *rockFootprint) contains(p geom.V) bool {
	inside := false
	const epsilon = 1e-10
	f.faces.Query(geom.Bounds{Min: p.Sub(geom.V{X: epsilon, Y: epsilon}), Max: p.Add(geom.V{X: epsilon, Y: epsilon})}, func(i int) bool {
		poly := f.grid[i].Polygon
		inside = geom.InsidePolygon(p, poly)
		if !inside {
			for j, a := range poly {
				if geom.SegmentDistanceSquared(p, p, a, poly[(j+1)%len(poly)]) < epsilon*epsilon {
					inside = true
					break
				}
			}
		}
		if inside {
			return false
		}
		return true
	})
	return inside
}

func (f *rockFootprint) backgroundHeight(p geom.V) float64 {
	return rockContourHeight + max(backgroundRockPadding-f.distance(p), f.backgroundBorderHeight(p))
}

func (f *rockFootprint) backgroundBorderHeight(p geom.V) float64 {
	// Continuous border rock reaches the outer fade in either orientation.
	// Unequal waves keep the inner edges from forming straight parallel rails.
	y := p.Y + f.worldTop
	left := backgroundBorderWidth + .025*math.Sin(y*11) + .012*math.Sin(y*29+1)
	right := backgroundBorderWidth + .025*math.Sin(y*13+2) + .012*math.Sin(y*31)
	return max(left-p.X, right-(generationWidth-p.X))
}

func (f *rockFootprint) backgroundContains(p geom.V) bool {
	// Most cells are in solid border strips. They need neither a foreground
	// lookup nor the wavy inner-border calculation to establish occupancy.
	return p.X <= backgroundSolidBorderWidth || p.X >= generationWidth-backgroundSolidBorderWidth || f.backgroundBorderHeight(p) >= 0 || f.distance(p) <= backgroundRockPadding
}

type backgroundPaddingHole struct {
	polygon []geom.V
	bounds  geom.Bounds
}

// World-keyed, jittered pockets vary in size, angle, and outline. Each window
// includes the neighboring rows so pockets crossing a section seam stay whole.
func backgroundPaddingHoles(seed int64, top float64) []backgroundPaddingHole {
	const stepX, stepY = .24, .28
	seed ^= 0x72656172686f6c65
	var holes []backgroundPaddingHole
	for row := int64(math.Floor((top+GenerationMinY)/stepY)) - 1; float64(row)*stepY < top+generationMaxY+stepY; row++ {
		for col := int64(math.Floor(BackgroundMinX / stepX)); float64(col)*stepX < BackgroundMaxX; col++ {
			random := func(channel int64) float64 { return SiteRandom(seed, row, col, channel) }
			if random(0) > .65 {
				continue
			}
			center := geom.V{X: (float64(col) + .25 + .5*random(1)) * stepX, Y: (float64(row)+.25+.5*random(2))*stepY - top}
			rx, ry := .035+.030*random(3), .040+.035*random(4)
			rotation := 2 * math.Pi * random(5)
			count := 9 + int(5*random(6))
			poly := make([]geom.V, count)
			for i := range poly {
				angle := 2 * math.Pi * float64(i) / float64(count)
				radius := .65 + .5*random(int64(7+i))
				poly[i] = center.Add(geom.Rotate(geom.V{X: rx * radius * math.Cos(angle), Y: ry * radius * math.Sin(angle)}, rotation))
			}
			lo, hi := geom.PolygonBounds(poly)
			holes = append(holes, backgroundPaddingHole{polygon: poly, bounds: geom.Bounds{Min: lo, Max: hi}})
		}
	}
	return holes
}

// Remove complete cells for angular rims matching the rear silhouette. A
// bounding disk protects the entire uncut foreground and a small backing lip,
// including thin branches crossing a cell whose vertices all lie outside rock.
func (f *rockFootprint) backgroundHoleCell(center geom.V, poly []geom.V, holes []backgroundPaddingHole) bool {
	for _, hole := range holes {
		if center.X < hole.bounds.Min.X || center.X > hole.bounds.Max.X || center.Y < hole.bounds.Min.Y || center.Y > hole.bounds.Max.Y || !geom.InsidePolygon(center, hole.polygon) {
			continue
		}
		radius := 0.0
		for _, p := range poly {
			radius = max(radius, p.Sub(center).Len())
		}
		return center.X+radius+.012 < f.minX || center.X-radius-.012 > f.maxX || f.distance(center) > radius+.012
	}
	return false
}

// Closely spaced loop walls can introduce a run of almost collinear shared
// junctions. Ear clipping cannot always resolve that run at its tolerance.
// Remove redundant points only on affected faces, preserving their silhouette;
// adjacency and collision already handle partial shared borders.
func cleanRockJunctions(grid RockGrid) {
	parallelFor(len(grid), func(i int) {
		poly := grid[i].Polygon
		if len(geom.Triangulate(poly)) == len(poly)-2 {
			return
		}
		clean := geom.CleanPolygon(poly)
		if len(clean) >= 3 && math.Abs(geom.PolygonArea(clean)-geom.PolygonArea(poly)) < 1e-13 && len(geom.Triangulate(clean)) == len(clean)-2 && simpleRockOutline(clean) {
			grid[i].Polygon = clean
		}
	})
}

// Clip the outer footprint through existing faces instead of exposing a row
// of complete Voronoi tiles. Keep the original face normal and material so
// clipping/triangulation cannot introduce extra wedges of light.
func contourRockGrid(grid RockGrid, heightAt func(geom.V) float64, guides ...[]Guide) RockGrid {
	grid = splitRockJunctions(grid)
	var boundaries []Guide
	if len(guides) > 0 {
		boundaries = guides[0]
	}
	parts := make([]RockGrid, len(grid))
	parallelFor(len(grid), func(n int) {
		c := grid[n]
		for _, poly := range contourRockFace(c.Polygon, c.Center, heightAt, boundaries) {
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

func contourRockFace(poly []geom.V, center geom.V, heightAt func(geom.V) float64, guides ...[]Guide) [][]geom.V {
	if len(poly) < 3 {
		return nil
	}
	values := make([]float64, len(poly))
	inside := 0
	for i, p := range poly {
		// On a guide the height is discontinuous. Sample just inside this
		// face so the two sides retain their own boundary elevation. Elsewhere
		// sample the shared vertex itself, independent of either face's center.
		q := p
		if len(guides) > 0 && len(guides[0]) > 0 {
			_, pr := nearestGuide(p, guides[0])
			if pr.Dist < mergeTolerance {
				q = geom.LerpVector(p, center, 1e-6)
				if !geom.InsidePolygon(q, poly) {
					prev, next := poly[(i+len(poly)-1)%len(poly)], poly[(i+1)%len(poly)]
					q = geom.LerpVector(p, prev.Add(next).Mul(.5), 1e-6)
				}
			}
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
	var triangles [][3]int
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
	if !fan {
		triangles = geom.Triangulate(poly)
	}
	var faces [][]geom.V
	for _, tri := range triangles {
		// Sampling triangle centers preserves narrow spurs passing through
		// a face whose perimeter vertices happen to be outside the footprint.
		mid := poly[tri[0]].Add(poly[tri[1]]).Add(poly[tri[2]]).Mul(1.0 / 3)
		z := heightAt(mid) - rockContourHeight
		var boundaries []Guide
		if len(guides) > 0 {
			boundaries = guides[0]
		}
		for k, a := range tri {
			b := tri[(k+1)%3]
			points := []geom.V{mid, poly[a], poly[b]}
			heights := []float64{z, values[a], values[b]}
			clipped := clipHeightTriangle(points, heights, heightAt, boundaries)
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

// Find crossings on the actual relief, which can flatten to zero outside a
// branch. Linear height interpolation there leaves protruding triangular teeth
// that subsequent gradient projection cannot remove. Starting from the solid
// endpoint makes the crossing independent of edge traversal direction.
func clipHeightTriangle(points []geom.V, values []float64, heightAt func(geom.V) float64, guides []Guide) []geom.V {
	var out []geom.V
	prev, zp := points[len(points)-1], values[len(values)-1]
	for i, p := range points {
		z := values[i]
		if (z >= 0) != (zp >= 0) {
			crossing := geom.LerpVector(prev, p, geom.Clamp(zp/(zp-z), 0, 1))
			sample := heightAt
			if _, pr := nearestGuide(crossing, guides); pr.Dist < mergeTolerance {
				// Guide heights jump across the lip. Keep root samples on this
				// triangle's side, just like its original boundary vertices.
				interior := points[0].Add(points[1]).Add(points[2]).Mul(1.0 / 3)
				sample = func(q geom.V) float64 {
					return heightAt(geom.LerpVector(q, interior, 1e-6))
				}
			}
			if math.Abs(sample(crossing)-rockContourHeight) >= 1e-12 {
				solid, empty := prev, p
				if zp < 0 {
					solid, empty = empty, solid
				}
				for iteration := 0; iteration < 40; iteration++ {
					mid := geom.LerpVector(solid, empty, .5)
					if sample(mid) >= rockContourHeight {
						solid = mid
					} else {
						empty = mid
					}
				}
				crossing = geom.LerpVector(solid, empty, .5)
			}
			out = append(out, crossing)
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
	copy(grid, splitRockJunctions(grid))
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
			changed := false
			for _, p := range c.Polygon {
				if _, ok := moves[keyAt(p)]; ok {
					changed = true
					break
				}
			}
			if !changed {
				return
			}
			poly := append([]geom.V(nil), c.Polygon...)
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

// Add fine silhouette detail after contour projection, which would otherwise
// smooth the chips away. Only exposed edges acquire vertices; shared edges and
// junctions remain fixed. Tooth height is capped in scene units, independent
// of facet length, so a long face cannot turn into a tall needle.
func roughenRockEdges(grid RockGrid, guides []Guide, seed int64, top float64, options ShapeOptions) {
	if options.SpikeDensity == 0 && options.EdgeRoughness == 0 {
		return
	}
	copy(grid, splitRockJunctions(grid))
	trimRockNeedles(grid, guides)
	edges := make(map[[4]int64]RockEdge)
	for _, edge := range ExposedRockEdges(grid) {
		edges[geom.EdgeKey(edge.A, edge.B)] = edge
	}
	parallelFor(len(grid), func(i int) {
		cell := &grid[i]
		var poly []geom.V
		for j, a := range cell.Polygon {
			b := cell.Polygon[(j+1)%len(cell.Polygon)]
			poly = append(poly, a)
			if _, ok := edges[geom.EdgeKey(a, b)]; !ok {
				continue
			}
			length := b.Sub(a).Len()
			if length < .006 || rockWindowEdge(a, b) {
				continue
			}
			mid := geom.LerpVector(a, b, .5)
			rng := rand.New(rand.NewSource(cellSeed(seed^0x6368697073, mid, top)))
			outward := b.Sub(a).Norm().Perp().Mul(-1)
			// Keep guide crests close to their authored line and grass roots.
			_, pr := nearestGuide(mid, guides)
			scale := 1.0
			if pr.Dist < .004 {
				scale = .35
			}
			count := max(1, int(math.Round(length/.012)))
			for n := 0; n < count; n++ {
				u := (float64(n) + geom.Lerp(.35, .65, rng.Float64())) / float64(count)
				p := geom.LerpVector(a, b, u)
				// Most changes are small inward chips, keeping a chunky angular
				// edge. Teeth are more frequent but only a few pixels high.
				height := options.EdgeRoughness * geom.Lerp(-.003, .001, rng.Float64())
				spike, size := rng.Float64(), geom.Lerp(.002, .005, rng.Float64())
				if spike < options.SpikeDensity {
					height += size
				}
				height *= scale * math.Min(1, length/.012)
				q := p.Add(outward.Mul(height))
				if q.X > 0 && q.X < generationWidth && q.Y > GenerationMinY && q.Y < generationMaxY {
					poly = append(poly, q)
				}
			}
		}
		// Concave or tiny contour faces can reject detail without changing any
		// neighboring junction. Keep only valid, triangulatable outlines.
		if geom.PolygonArea(poly) > .000001 && simpleRockOutline(poly) && len(geom.Triangulate(poly)) == len(poly)-2 {
			cell.Polygon = poly
			if !geom.InsidePolygon(cell.Center, poly) {
				cell.Center = geom.PolygonCenter(poly)
			}
		}
	})
}

// Remove isolated thin contour needles before adding the finer edge detail.
// Large branch tips survive. All faces incident on a tip move together, and
// every proposed move must leave those faces valid.
func trimRockNeedles(grid RockGrid, guides []Guide) {
	boundary := ExposedRockEdges(grid)
	type corner struct {
		previous, next     geom.V
		incoming, outgoing int
	}
	corners := make(map[[2]int64]corner)
	for _, edge := range boundary {
		a, b := CollisionVertexKey(edge.A), CollisionVertexKey(edge.B)
		c := corners[a]
		c.next = edge.B
		c.outgoing++
		corners[a] = c
		c = corners[b]
		c.previous = edge.A
		c.incoming++
		corners[b] = c
	}
	incident := make(map[[2]int64][]int)
	for i, c := range grid {
		for _, p := range c.Polygon {
			k := CollisionVertexKey(p)
			incident[k] = append(incident[k], i)
		}
	}
	for _, edge := range boundary {
		p := edge.B
		k := CollisionVertexKey(p)
		c := corners[k]
		if c.incoming != 1 || c.outgoing != 1 {
			continue
		}
		a, b := c.previous, c.next
		base := b.Sub(a)
		if base.Len2() == 0 || base.Len() > .015 {
			continue
		}
		u := geom.Clamp(p.Sub(a).Dot(base)/base.Len2(), 0, 1)
		foot := a.Add(base.Mul(u))
		height := p.Sub(foot).Len()
		if height < .006 || height > .03 || height < base.Len()*1.25 || geom.Cross(p.Sub(a), b.Sub(p)) <= 0 {
			continue
		}
		_, pr := nearestGuide(p, guides)
		if pr.Dist < .004 {
			continue
		}
		q := foot.Add(p.Sub(foot).Norm().Mul(.004))
		valid := true
		polys := make([][]geom.V, len(incident[k]))
		for n, i := range incident[k] {
			poly := append([]geom.V(nil), grid[i].Polygon...)
			for j, v := range poly {
				if CollisionVertexKey(v) == k {
					poly[j] = q
				}
			}
			if geom.PolygonArea(poly) < .000001 || !simpleRockOutline(poly) || len(geom.Triangulate(poly)) != len(poly)-2 {
				valid = false
				break
			}
			polys[n] = poly
		}
		if valid {
			for n, i := range incident[k] {
				grid[i].Polygon = polys[n]
				if !geom.InsidePolygon(grid[i].Center, polys[n]) {
					grid[i].Center = geom.PolygonCenter(polys[n])
				}
			}
		}
	}
}
