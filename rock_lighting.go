package infinicave

import "math"

// Screen Y increases downward; positive Z points toward the camera.
type V3 struct{ X, Y, Z float64 }

func (v V3) Norm() V3 {
	length := math.Sqrt(v.X*v.X + v.Y*v.Y + v.Z*v.Z)
	if length == 0 {
		return V3{Z: 1}
	}
	return V3{v.X / length, v.Y / length, v.Z / length}
}

func surfaceLight(normal V3) float64 {
	return math.Max(0, rockLight.X*normal.X+rockLight.Y*normal.Y+rockLight.Z*normal.Z)
}

// Heights vary at rock and shoulder scales. Sampling world coordinates keeps
// both layers stable across overlapping windows, reloads, and cache eviction.
func rockHeight(p V, noise *Perlin) float64 {
	p.Y += noise.OffsetY
	return -.008 + .004*noise.Noise(p.X*6, p.Y*6) + .001*noise.Noise(p.X*25+73, p.Y*25+149)
}

func shapeRockGrid(grid RockGrid, guides []Guide, noise *Perlin) {
	if len(guides) > 0 {
		shapeReliefGrid(grid, guides, noise, nil)
		return
	}
	for i := range grid {
		grid[i].Z = rockHeight(grid[i].Center, noise)
	}
	neighbors := rockNeighbors(grid)
	for i := range grid {
		grid[i].Normal = rockNormal(grid, i, neighbors[i])
		if toward, ok := guideFacing(grid[i], guides); ok {
			// The guide overrides the height slope, retaining a visible front.
			grid[i].Normal = V3{toward.X * 1.4, toward.Y * 1.4, 1}.Norm()
		}
	}
}

// Match shared boundary lengths, including partial edges left by guide cuts
// and merged fragments. Corner contact alone does not make cells neighbors.
func rockNeighbors(grid RockGrid) [][]int {
	bounds := make([]guideFragment, len(grid))
	for i, c := range grid {
		bounds[i] = makeGuideFragment(c.Polygon, c.Center, false)
	}
	neighbors := make([][]int, len(grid))
	for i, a := range bounds {
		for j := i + 1; j < len(bounds); j++ {
			b := bounds[j]
			if a.lo.X > b.hi.X+mergeTolerance || a.hi.X < b.lo.X-mergeTolerance ||
				a.lo.Y > b.hi.Y+mergeTolerance || a.hi.Y < b.lo.Y-mergeTolerance {
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
func rockNormal(grid RockGrid, i int, neighbors []int) V3 {
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
	return V3{-gx, -gy, 1}.Norm()
}

// Only guides actually touching a face can override its slope. For multiple
// guides or a curl, face the nearest contact. Interior tips count as contact.
func guideFacing(cell RockCell, guides []Guide) (V, bool) {
	best2 := math.Inf(1)
	var toward V
	bounds := makeGuideFragment(cell.Polygon, cell.Center, false)
	consider := func(q V) {
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
		if insideFace(pr.Q, cell.Polygon) {
			consider(pr.Q)
		}
		for j, a := range cell.Polygon {
			d := cell.Polygon[(j+1)%len(cell.Polygon)].Sub(a)
			if d.Len2() < 1e-18 {
				continue
			}
			q := a.Add(d.Mul(clamp(cell.Center.Sub(a).Dot(d)/d.Len2(), 0, 1)))
			if g.distanceBound2(q) <= mergeTolerance*mergeTolerance && g.project(q).Dist < mergeTolerance {
				consider(q)
			}
		}
	}
	return toward, !math.IsInf(best2, 1)
}
