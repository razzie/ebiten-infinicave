package terrain

import (
	"math"
	"sort"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func SurfaceLight(normal geom.V3, orientation ...Orientation) float64 {
	normal = WorldNormal(OptionalOrientation(orientation), normal)
	return math.Max(0, rockLight.X*normal.X+rockLight.Y*normal.Y+rockLight.Z*normal.Z)
}

// Heights vary at rock and shoulder scales. Sampling world coordinates keeps
// both layers stable across overlapping windows, reloads, and cache eviction.
func rockHeight(p geom.V, noise *Perlin) float64 {
	p.Y += noise.OffsetY
	return -.008 + .004*noise.Noise(p.X*6, p.Y*6) + .001*noise.Noise(p.X*25+73, p.Y*25+149)
}

func shapeRockGrid(grid RockGrid, guides []Guide, noise *Perlin) [][]int {
	if len(guides) > 0 {
		shapeReliefGrid(grid, guides, noise, nil)
		return nil
	}
	parallelFor(len(grid), func(i int) { grid[i].Z = rockHeight(grid[i].Center, noise) })
	neighbors := RockNeighbors(grid)
	parallelFor(len(grid), func(i int) { grid[i].Normal = rockNormal(grid, i, neighbors[i]) })
	return neighbors
}

// Match shared boundary lengths, including partial edges left by guide cuts
// and merged fragments. Corner contact alone does not make cells neighbors.
func RockNeighbors(grid RockGrid) [][]int {
	const bucketSize = .05
	type bin [2]int
	bounds := make([]guideFragment, len(grid))
	buckets := make(map[bin][]int)
	bins := make([][2]bin, len(grid))
	for i, c := range grid {
		bounds[i] = makeGuideFragment(c.Polygon, c.Center, false)
		if len(c.Polygon) < 3 {
			continue
		}
		a := bounds[i]
		lo := bin{int(math.Floor((a.lo.X - mergeTolerance) / bucketSize)), int(math.Floor((a.lo.Y - mergeTolerance) / bucketSize))}
		hi := bin{int(math.Floor((a.hi.X + mergeTolerance) / bucketSize)), int(math.Floor((a.hi.Y + mergeTolerance) / bucketSize))}
		bins[i] = [2]bin{lo, hi}
		for y := lo[1]; y <= hi[1]; y++ {
			for x := lo[0]; x <= hi[0]; x++ {
				key := bin{x, y}
				buckets[key] = append(buckets[key], i)
			}
		}
	}
	neighbors := make([][]int, len(grid))
	seen := make([]int, len(grid))
	candidates := make([]int, 0, 32)
	for i, a := range bounds {
		if len(a.poly) < 3 {
			continue
		}
		candidates = candidates[:0]
		lo, hi := bins[i][0], bins[i][1]
		for y := lo[1]; y <= hi[1]; y++ {
			for x := lo[0]; x <= hi[0]; x++ {
				for _, j := range buckets[bin{x, y}] {
					if j > i && seen[j] != i+1 {
						seen[j] = i + 1
						candidates = append(candidates, j)
					}
				}
			}
		}
		// Preserve the previous site-order sums when fitting normals.
		sort.Ints(candidates)
		for _, j := range candidates {
			b := bounds[j]
			if a.lo.X > b.hi.X+mergeTolerance || a.hi.X < b.lo.X-mergeTolerance || a.lo.Y > b.hi.Y+mergeTolerance || a.hi.Y < b.lo.Y-mergeTolerance {
				continue
			}
			if mergeableBorder(a.poly, b.poly, nil) > mergeTolerance {
				neighbors[i] = append(neighbors[i], j)
				neighbors[j] = append(neighbors[j], i)
			}
		}
	}
	return neighbors
}

// Fit z = ownZ + gx*dx + gy*dy to neighboring control points. Inverse squared
// distance weights give each direction equal influence despite unequal cells.
// A rising height toward the bottom gives normal.Y < 0: an upward-facing rock.
func rockNormal(grid RockGrid, i int, neighbors []int) geom.V3 {
	var xx, xy, yy, xz, yz float64
	for _, j := range neighbors {
		d := grid[j].Center.Sub(grid[i].Center)
		if d.Len2() < 1e-18 {
			continue
		}
		w, dz := 1/d.Len2(), grid[j].Z-grid[i].Z
		xx += w * d.X * d.X
		xy += w * d.X * d.Y
		yy += w * d.Y * d.Y
		xz += w * d.X * dz
		yz += w * d.Y * dz
	}
	gx, gy := 0.0, 0.0
	det := xx*yy - xy*xy
	if det > 1e-10*(xx+yy)*(xx+yy) {
		gx, gy = (yy*xz-xy*yz)/det, (xx*yz-xy*xz)/det
	} else if trace := xx + yy; trace > 0 {
		// One neighbor or collinear neighbors: use the constrained slope,
		// leaving the unsupported perpendicular direction flat.
		gx, gy = xz/trace, yz/trace
	}
	return geom.V3{X: -gx, Y: -gy, Z: 1}.Norm()
}

// Only guides actually touching a face can override its slope. For multiple
// guides or a curl, face the nearest contact. Interior tips count as contact.
func guideFacing(cell RockCell, guides []Guide) (geom.V, bool) {
	best2 := math.Inf(1)
	var toward geom.V
	bounds := makeGuideFragment(cell.Polygon, cell.Center, false)
	consider := func(q geom.V) {
		d := q.Sub(cell.Center)
		if d.Len2() > 1e-18 && d.Len2() < best2 {
			best2, toward = d.Len2(), d.Norm()
		}
	}
	for i := range guides {
		g := &guides[i]
		if g.Max.X < bounds.lo.X-mergeTolerance || g.Min.X > bounds.hi.X+mergeTolerance ||
			g.Max.Y < bounds.lo.Y-mergeTolerance || g.Min.Y > bounds.hi.Y+mergeTolerance {
			continue
		}
		pr := g.project(cell.Center)
		if geom.InsidePolygon(pr.Q, cell.Polygon) {
			consider(pr.Q)
		}
		for j, a := range cell.Polygon {
			d := cell.Polygon[(j+1)%len(cell.Polygon)].Sub(a)
			if d.Len2() < 1e-18 {
				continue
			}
			q := a.Add(d.Mul(geom.Clamp(cell.Center.Sub(a).Dot(d)/d.Len2(), 0, 1)))
			if g.distanceBound2(q) <= mergeTolerance*mergeTolerance && g.project(q).Dist < mergeTolerance {
				consider(q)
			}
		}
	}
	return toward, !math.IsInf(best2, 1)
}
