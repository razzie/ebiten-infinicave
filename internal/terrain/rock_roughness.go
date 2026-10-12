package terrain

import (
	"math"
	"math/rand"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

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
