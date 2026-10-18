package terrain

import (
	"image/color"
	"math"
	"math/rand"
	"sort"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

const (
	foregroundEdgeFadeWidth = .140
	rockReliefChipHeight    = .0012
)

// All terrain lighting, including exposed walls, uses this one light.
var rockLight = (geom.V3{X: -.28, Y: -.86, Z: .65}).Norm()

// RockLightDirection expresses the shared light in the section's render frame.
func RockLightDirection(orientation Orientation) geom.V3 {
	p := InternalPoint(orientation, geom.V{X: rockLight.X, Y: rockLight.Y})
	return geom.V3{X: p.X, Y: p.Y, Z: rockLight.Z}
}

// The guide is the exposed lip. Just inside it the bevel rises to a crest,
// then a much wider flank descends toward the surrounding recessed terrain.
func ridgeProfile(distance, width, height, bevel float64) float64 {
	if distance < 0 || distance >= width {
		return 0
	}
	if distance < bevel {
		return height * geom.Lerp(.55, 1, distance/bevel)
	}
	// The flank steepens toward the foot, turning into shadow instead of
	// flattening back into a bright row of lower cells.
	return height * math.Pow(1-(distance-bevel)/(width-bevel), .72)
}

func reliefHeight(p geom.V, guides []Guide, noise *Perlin, branches *BranchField) float64 {
	height, branchMask := 0.0, 1.0
	for i := range guides {
		g := &guides[i]
		if g.distanceBound2(p) > 0.16*0.16 {
			continue
		}
		pr, near := g.projectWithin(p, .16)
		if !near {
			continue
		}
		if pr.Signed < 0 {
			branchMask *= geom.Smoothstep(guideSpacing*2.7, guideSpacing*5, pr.Dist)
			continue
		}
		// Width and elevation vary along the crest, not independently per face.
		q := pr.Q
		q.Y += noise.OffsetY
		variation := noise.Noise(q.X*4+17, q.Y*4+31)
		width := 0.105 + 0.035*variation
		elevation := 0.062 + 0.014*variation
		bevel := 0.016 + 0.004*variation
		// Fade along the actual guide before its tips. Extending a raised
		// half-cap beyond an open tip introduces a discontinuity that no
		// guide edge cuts, leaving detached wedges when faces are sampled.
		end := geom.Smoothstep(0, 0.038, math.Min(pr.S, g.S[len(g.S)-1]-pr.S))
		height = math.Max(height, ridgeProfile(pr.Dist, width, elevation, bevel)*end)
	}
	// Offshoots carry low connected spurs of the same solid rock surface.
	positive := branchBias(p, branches) * branchMask
	negative := branchNegativeBias(p, branches, positive, height, noise)
	height = math.Max(0, math.Max(height, 0.08*positive)-negative)
	world := geom.V{X: p.X, Y: p.Y + noise.OffsetY}
	chips := rockReliefChipHeight * noise.Noise(world.X*35+43, world.Y*35+97)
	edgeFade := geom.Smoothstep(0, foregroundEdgeFadeWidth, math.Min(p.X, generationWidth-p.X))
	return math.Max(0, height+chips*geom.Smoothstep(0, 0.015, height)) * edgeFade
}

// Compress site spacing across the lip and stretch it into the flank. The
// continuous displacement retains irregular sites and creates no aligned rows.
func reliefSeeds(seeds []geom.V, guides []Guide) []geom.V {
	out := append([]geom.V(nil), seeds...)
	for i, p := range out {
		gi, pr := nearestGuide(p, guides)
		if gi < 0 || pr.Signed <= 0 || pr.Dist >= 0.11 {
			continue
		}
		shift := -0.015 * math.Sin(math.Pi*pr.Dist/0.11)
		out[i] = p.Add(pr.N.Mul(shift))
		out[i].X = geom.Clamp(out[i].X, .001, generationWidth-.001)
		out[i].Y = geom.Clamp(out[i].Y, GenerationMinY+.001, generationMaxY-.001)
	}
	return out
}

func shapeReliefGrid(grid RockGrid, guides []Guide, noise *Perlin, branches *BranchField) {
	parallelFor(len(grid), func(i int) {
		grid[i].Z = reliefHeight(grid[i].Center, guides, noise, branches)
		grid[i].Raised = true
	})
	neighbors := RockNeighbors(grid)
	parallelFor(len(grid), func(i int) {
		grid[i].Normal = rockNormal(grid, i, neighbors[i])
		if toward, ok := guideFacing(grid[i], guides); ok {
			grid[i].Normal = (geom.V3{X: toward.X * 1.4, Y: toward.Y * 1.4, Z: 1}).Norm()
		}
	})
}

func RockSurfaceColor(normal geom.V3, shadow, ambient float64, orientation ...Orientation) color.NRGBA {
	// Material reflectance is separate from visibility and distance to a guide.
	// A low ambient floor keeps flanks charcoal; only lit faces reach warm tan.
	diffuse := math.Pow(SurfaceLight(normal, orientation...), 1.25)
	return cellColor(.16 + .05*ambient + .66*diffuse*shadow)
}

func shadeRockGrids(background, foreground RockGrid, noise *Perlin, orientation ...Orientation) {
	mode := OptionalOrientation(orientation)
	for _, grid := range []RockGrid{background, foreground} {
		for i := range grid {
			grid[i].orientation = mode
			grid[i].worldTop = noise.OffsetY
		}
	}
	depth := newRockDepth(background, foreground)
	for layer, grid := range []RockGrid{background, foreground} {
		parallelFor(len(grid), func(i int) {
			c := &grid[i]
			if layer == 1 {
				c.Shadow, c.Ambient = depth.illumination(*c)
				c.Color = RockSurfaceColor(c.Normal, c.Shadow, c.Ambient, mode)
				// Weathering is coherent across a formation. Lower spurs and
				// recessed feet stay subdued; only the raised crests catch ivory.
				patina := .72 + .28*geom.Smoothstep(0.005, 0.065, c.Z)
				p := geom.V{X: c.Center.X, Y: c.Center.Y + noise.OffsetY}
				patina *= .96 + .06*noise.Noise(p.X*8+21, p.Y*8+47)
				c.Color.R = uint8(math.Round(float64(c.Color.R) * patina))
				c.Color.G = uint8(math.Round(float64(c.Color.G) * patina * .985))
				c.Color.B = uint8(math.Round(float64(c.Color.B) * patina * .955))
			} else {
				// Background cast shadows are composited from the current rock
				// silhouette at draw time, including carved openings and vines.
				c.Shadow, c.Ambient = 1, 1
				c.Color = backgroundSurfaceColor(c.Center, noise, c.Normal, mode)
			}
		})
	}
}

// Quiet flanks use fewer, larger facets than the chipped lip. Thinning is
// keyed to world sites so revisiting or overlapping a section changes nothing.
func artisticRockSeeds(seeds []geom.V, guides []Guide, seed int64, top float64) []geom.V {
	selected := make([]geom.V, 0, len(seeds))
	for _, p := range seeds {
		gi, pr := nearestGuide(p, guides)
		remove := 0.0
		if gi >= 0 && pr.Signed > 0 {
			remove = .15 * geom.Smoothstep(0.025, 0.095, pr.Dist) * (1 - geom.Smoothstep(0.19, 0.26, pr.Dist))
		}
		r := float64(uint64(cellSeed(seed^0x617274, p, top))>>11) / (1 << 53)
		if r >= remove {
			selected = append(selected, p)
		}
	}
	return reliefSeeds(selected, guides)
}

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

const rockDepthStep = .004

const rockDepthWidth = int(generationWidth / rockDepthStep)

const rockDepthHeight = int(GenerationHeight / rockDepthStep)

type rockDepth struct {
	heights   []float64
	light     geom.V3
	direction geom.V
	rise      float64
	probes    []shadowProbe
}

// Each face remains one plane, including concave faces. Clamp extrapolation
// at long polygon tips so fitted slopes cannot create implausible spikes.
func RockDepthAt(c RockCell, p geom.V) float64 {
	if c.Normal.Z < .05 {
		return c.Z
	}
	d := p.Sub(c.Center)
	z := c.Z - (d.X*c.Normal.X+d.Y*c.Normal.Y)/c.Normal.Z
	if c.Raised {
		return geom.Clamp(z, math.Max(.001, c.Z-.028), c.Z+.028)
	}
	return geom.Clamp(z, c.Z-.004, c.Z+.004)
}

// Rasterize real faces into a shared height buffer. world-aligned samples and
// the generation padding keep shadow queries identical across section seams.
func newRockDepth(grids ...RockGrid) *rockDepth {
	d := &rockDepth{heights: make([]float64, rockDepthWidth*rockDepthHeight)}
	d.light = rockLight
	for _, grid := range grids {
		if len(grid) > 0 {
			light := InternalPoint(grid[0].orientation, geom.V{X: rockLight.X, Y: rockLight.Y})
			d.light = geom.V3{X: light.X, Y: light.Y, Z: rockLight.Z}
			break
		}
	}
	d.direction = (geom.V{X: d.light.X, Y: d.light.Y}).Norm()
	d.rise = d.light.Z / math.Hypot(d.light.X, d.light.Y)
	for distance := .008; distance <= .184; distance += .004 {
		d.probes = append(d.probes, shadowProbe{offset: d.direction.Mul(distance), rise: distance * d.rise, softness: .005 + distance*.05})
	}
	fillFloat64(d.heights, -.016)
	crossings := make([]float64, 0, 16)
	for _, grid := range grids {
		for _, c := range grid {
			minY, maxY := float64(generationMaxY), float64(GenerationMinY)
			for _, p := range c.Polygon {
				minY, maxY = math.Min(minY, p.Y), math.Max(maxY, p.Y)
			}

			for y := max(0, int((minY-GenerationMinY)/rockDepthStep)); y < min(rockDepthHeight, int((maxY-GenerationMinY)/rockDepthStep)+1); y++ {
				py := GenerationMinY + (float64(y)+.5)*rockDepthStep
				crossings = crossings[:0]
				for j, a := range c.Polygon {
					b := c.Polygon[(j+1)%len(c.Polygon)]
					// Shared edges on a scan line belong to the same face in
					// every padded window, despite translation roundoff.
					if (a.Y <= py+1e-10 && b.Y > py+1e-10) || (b.Y <= py+1e-10 && a.Y > py+1e-10) {
						crossings = append(crossings, a.X+(b.X-a.X)*(py-a.Y)/(b.Y-a.Y))
					}
				}
				sort.Float64s(crossings)
				for j := 0; j+1 < len(crossings); j += 2 {
					first := max(0, int(math.Ceil(crossings[j]/rockDepthStep-.5-1e-9)))
					last := min(rockDepthWidth, int(math.Ceil(crossings[j+1]/rockDepthStep-.5-1e-9)))
					if first < last {
						rasterDepthSpan(d.heights[y*rockDepthWidth+first:y*rockDepthWidth+last], first, py, c)
					}
				}
			}
		}
	}
	return d
}

func (d *rockDepth) at(p geom.V) float64 {
	if p.X < 0 || p.X >= generationWidth || p.Y < GenerationMinY || p.Y >= generationMaxY {
		return -.016
	}
	// Equivalent padded windows can differ by a few floating-point bits at
	// a texel boundary. Resolve those ties identically before indexing.
	x := min(rockDepthWidth-1, int(math.Floor(p.X/rockDepthStep+1e-9)))
	y := min(rockDepthHeight-1, int(math.Floor((p.Y-GenerationMinY)/rockDepthStep+1e-9)))
	return d.heights[y*rockDepthWidth+x]
}

type shadowProbe struct {
	offset         geom.V
	rise, softness float64
}

func (d *rockDepth) visibility(p geom.V, z float64) float64 {
	visible := 1.0
	for _, probe := range d.probes {
		blocker := d.at(p.Add(probe.offset)) - z - probe.rise
		// A small bias avoids self-shadow acne; increasing softness models a
		// finite light source and leaves a sharper contact near the blocker.
		visible = math.Min(visible, 1-geom.Smoothstep(.002, probe.softness, blocker))
		if visible == 0 {
			break
		}
	}
	return visible
}

var ambientDirections = func() (directions [8]geom.V) {
	for i := range directions {
		angle := float64(i) * math.Pi / 4
		directions[i] = geom.V{X: math.Cos(angle), Y: math.Sin(angle)}
	}
	return
}()

var ambientProbes = func() (probes [8][3]struct {
	offset   geom.V
	distance float64
}) {
	for i, direction := range ambientDirections {
		for j, distance := range [...]float64{.008, .020, .040} {
			probes[i][j].offset = direction.Mul(distance)
			probes[i][j].distance = distance
		}
	}
	return
}()

func (d *rockDepth) ambient(p geom.V, z float64) float64 {
	occlusion := 0.0
	for _, probes := range ambientProbes {
		horizon := 0.0
		for _, probe := range probes {
			horizon = math.Max(horizon, (d.at(p.Add(probe.offset))-z-.003)/probe.distance)
		}
		occlusion += geom.Clamp(horizon, 0, 1)
	}
	return 1 - .75*occlusion/8
}

func (d *rockDepth) illumination(c RockCell) (shadow, ambient float64) {
	// Sample within the face, averaging visibility without introducing fan
	// triangles into the lighting. The polygon receives one flat-shaded tone.
	points := []geom.V{c.Center}
	for _, p := range c.Polygon {
		q := geom.LerpVector(c.Center, p, .6)
		if geom.InsidePolygon(q, c.Polygon) {
			points = append(points, q)
		}
	}
	for _, p := range points {
		z := RockDepthAt(c, p)
		shadow += d.visibility(p, z)
		ambient += d.ambient(p, z)
	}
	return shadow / float64(len(points)), ambient / float64(len(points))
}

func cellColor(v float64) color.NRGBA {
	// Sampled rock colors stay charcoal, with restrained warm-gray highlights.
	stops := []struct {
		value   float64
		r, g, b float64
	}{
		{0, 0, 0, 0},
		{.10, 8, 8, 7},
		{.25, 18, 18, 16},
		{.43, 38, 37, 34},
		{.65, 120, 116, 106},
		{.85, 165, 158, 142},
		{1, 190, 182, 164},
	}
	v = geom.Clamp(v, 0, 1)
	for i := 1; i < len(stops); i++ {
		a, b := stops[i-1], stops[i]
		if v <= b.value {
			t := (v - a.value) / (b.value - a.value)
			return color.NRGBA{uint8(math.Round(geom.Lerp(a.r, b.r, t))), uint8(math.Round(geom.Lerp(a.g, b.g, t))), uint8(math.Round(geom.Lerp(a.b, b.b, t))), 255}
		}
	}
	return color.NRGBA{221, 215, 200, 255}
}

// Occupancy already follows the padded foreground footprint. Noise varies the
// charcoal material without erasing that silhouette with unrelated black patches;
// the renderer fades the outer cross-axis margins into blackness.
func backgroundCellColor(p geom.V, noise *Perlin) color.NRGBA {
	return backgroundSurfaceColor(p, noise, geom.V3{Z: 1})
}

func backgroundSurfaceColor(p geom.V, noise *Perlin, normal geom.V3, orientation ...Orientation) color.NRGBA {
	material := geom.Lerp(.65, 1, geom.Smoothstep(.43, .59, fbm(noise, p)))
	return cellColor(.38 * material * (.28 + .72*SurfaceLight(normal, orientation...)))
}

// Rock occupancy follows relief, independently of light. Every existing face
// is opaque, including its dark flank and the tapered ends of raised spurs.
func guideCellColor(p geom.V, guides []Guide, noise *Perlin, branches *BranchField) color.NRGBA {
	dx := reliefHeight(p.Add(geom.V{X: .002, Y: 0}), guides, noise, branches) - reliefHeight(p.Sub(geom.V{X: .002, Y: 0}), guides, noise, branches)
	dy := reliefHeight(p.Add(geom.V{X: 0, Y: .002}), guides, noise, branches) - reliefHeight(p.Sub(geom.V{X: 0, Y: .002}), guides, noise, branches)
	return guideSurfaceColor(p, guides, noise, branches, (geom.V3{X: -dx / .004, Y: -dy / .004, Z: 1}).Norm())
}

func guideSurfaceColor(p geom.V, guides []Guide, noise *Perlin, branches *BranchField, normal geom.V3) color.NRGBA {
	if reliefHeight(p, guides, noise, branches) <= rockContourHeight {
		return color.NRGBA{}
	}
	return RockSurfaceColor(normal, 1, 1)
}

// ShadeRockFaces prepares a private rendering copy. Generation colors continue
// to steer vegetation; visual normals also drive the rendered depth shadows.
// Polygons and control-point heights are never changed by this material pass.
func ShadeRockFaces(grid RockGrid, boundary []RockEdge, adjacency ...[][]int) RockGrid {
	if len(grid) == 0 {
		return grid
	}
	if !grid[0].Raised {
		return shadeBackgroundRockFaces(grid, adjacency...)
	}
	faces := append(RockGrid(nil), grid...)
	noise := NewPerlin(rand.New(rand.NewSource(0x6661636574)))
	type crest struct {
		a, b, outward geom.V
		light         float64
	}
	var crests []crest
	for _, edge := range boundary {
		if edge.B.Sub(edge.A).Len() < .002 {
			continue // Tiny clipping slivers cannot light a whole neighboring face.
		}
		outward := edge.B.Sub(edge.A).Perp().Norm().Mul(-1)
		world := WorldPoint(grid[edge.Cell].orientation, outward)
		// Undersides and vertical walls never acquire an illuminated top band.
		up := geom.Smoothstep(.15, .75, -world.Y)
		facing := geom.Smoothstep(.1, .65, world.Dot((geom.V{X: rockLight.X, Y: rockLight.Y}).Norm()))
		if light := up * facing; light > 0 {
			crests = append(crests, crest{edge.A, edge.B, outward, light})
		}
	}
	emphasis := make([]float64, len(faces))
	parallelFor(len(faces), func(i int) {
		c := &faces[i]
		// A shared broad field dominates the small independent facet tilt.
		tilt := rockFacetTilt(*c, noise)
		width := geom.Clamp(2*math.Sqrt(geom.PolygonArea(c.Polygon)), .028, .065)
		strongest := 0.0
		var toward geom.V
		for _, edge := range crests {
			if c.Center.X < math.Min(edge.a.X, edge.b.X)-width || c.Center.X > math.Max(edge.a.X, edge.b.X)+width ||
				c.Center.Y < math.Min(edge.a.Y, edge.b.Y)-width || c.Center.Y > math.Max(edge.a.Y, edge.b.Y)+width {
				continue
			}
			d := edge.b.Sub(edge.a)
			q := edge.a.Add(d.Mul(geom.Clamp(c.Center.Sub(edge.a).Dot(d)/math.Max(d.Len2(), 1e-18), 0, 1)))
			if c.Center.Sub(q).Dot(edge.outward) > 1e-7 {
				continue
			}
			weight := edge.light * (1 - geom.Smoothstep(0, width, c.Center.Sub(q).Len()))
			strongest = math.Max(strongest, weight)
			// Blend directions at shared corners. Picking one equally near
			// segment can flip the tilt after harmless window roundoff.
			toward = toward.Add(edge.outward.Mul(weight * d.Len()))
		}
		toward = toward.Norm()
		n := c.Normal
		if n.Z < .05 {
			n = geom.V3{Z: 1}
		}
		// Preserve the relief slope, gently turning the top band toward its lip.
		c.Normal = (geom.V3{X: n.X/n.Z + tilt.X + .32*strongest*toward.X, Y: n.Y/n.Z + tilt.Y + .32*strongest*toward.Y, Z: 1}).Norm()
		emphasis[i] = strongest
	})
	depth := newRockDepth(faces)
	parallelFor(len(faces), func(i int) {
		c := &faces[i]
		c.Shadow, c.Ambient = depth.illumination(*c)
		// Keep some readable flank facets. Depth shapes direct illumination,
		// while ambient light is independently darkened by nearby occluders.
		flank := .72 + .28*geom.Smoothstep(.008, .062, c.Z)
		direct := math.Pow(SurfaceLight(c.Normal, c.orientation), 1.25) * c.Shadow
		direct *= flank * (1 + .12*emphasis[i])
		c.Color = cellColor(.13 + .08*c.Ambient + .66*direct)
		p := RockMaterialPoint(*c, c.Center)
		patina := .95 + .055*noise.Noise(p.X*8+21, p.Y*8+47)
		c.Color = color.NRGBA{
			R: uint8(math.Round(float64(c.Color.R) * patina)),
			G: uint8(math.Round(float64(c.Color.G) * patina * .985)),
			B: uint8(math.Round(float64(c.Color.B) * patina * .955)), A: grid[i].Color.A,
		}
	})
	return faces
}

func rockFacetTilt(c RockCell, noise *Perlin) geom.V {
	p := RockMaterialPoint(c, c.Center)
	key := CollisionVertexKey(p)
	tilt := geom.V{
		X: .16*noise.Noise(p.X*5+17, p.Y*5+31) + .10*(SiteRandom(0x6661636574, key[0], key[1], 0)-.5),
		Y: .16*noise.Noise(p.X*5+73, p.Y*5+97) + .10*(SiteRandom(0x6661636574, key[0], key[1], 1)-.5),
	}
	return InternalPoint(c.orientation, tilt)
}

// The recessed background has no exposed platform crests. Retain its broad
// material field and exact black pockets, then refine its shallow relief with
// correlated normals and restrained contact occlusion. Neighbor probes cover
// the extended side margins as well as the cave's central square.
func shadeBackgroundRockFaces(grid RockGrid, adjacency ...[][]int) RockGrid {
	faces := append(RockGrid(nil), grid...)
	noise := NewPerlin(rand.New(rand.NewSource(0x6661636574)))
	parallelFor(len(faces), func(i int) {
		c := &faces[i]
		tilt := rockFacetTilt(*c, noise).Mul(2)
		n := c.Normal
		if n.Z < .05 {
			n = geom.V3{Z: 1}
		}
		c.Normal = (geom.V3{X: 1.4*n.X/n.Z + tilt.X, Y: 1.4*n.Y/n.Z + tilt.Y, Z: 1}).Norm()
	})
	var neighbors [][]int
	if len(adjacency) > 0 {
		neighbors = adjacency[0]
	}
	if neighbors == nil {
		neighbors = RockNeighbors(faces)
	}
	// Normals are immutable during the second pass; only each worker's own
	// output color/illumination changes. Read neighbor material from the input.
	parallelFor(len(faces), func(i int) {
		c := &faces[i]
		occlusion := 0.0
		for _, j := range neighbors[i] {
			if grid[j].Color.A == 0 || grid[j].Color.R == 0 && grid[j].Color.G == 0 && grid[j].Color.B == 0 {
				continue
			}
			other := RockCell{Center: faces[j].Center, Normal: faces[j].Normal, Z: faces[j].Z}
			d := other.Center.Sub(c.Center)
			mid := c.Center.Add(d.Mul(.5))
			step := RockDepthAt(other, mid) - RockDepthAt(*c, mid) - .0005
			occlusion += geom.Clamp(step/math.Max(.008, d.Len()), 0, .35)
		}
		c.Ambient = 1 - math.Min(.18, 2*occlusion/float64(max(1, len(neighbors[i]))))
		c.Shadow = 1 // Foreground cast shadows remain a dynamic composite pass.
		oldLight := .3 + .7*math.Pow(SurfaceLight(grid[i].Normal.Norm(), c.orientation), 1.25)
		newLight := .3 + .7*math.Pow(SurfaceLight(c.Normal, c.orientation), 1.25)
		p := RockMaterialPoint(*c, c.Center)
		patina := .98 + .04*noise.Noise(p.X*8+21, p.Y*8+47)
		depth := .9 + .1*geom.Smoothstep(-.012, -.004, c.Z)
		strength := newLight / oldLight * c.Ambient * depth * patina
		base := grid[i].Color
		c.Color = color.NRGBA{
			R: uint8(math.Round(geom.Clamp(float64(base.R)*strength, 0, 255))),
			G: uint8(math.Round(geom.Clamp(float64(base.G)*strength, 0, 255))),
			B: uint8(math.Round(geom.Clamp(float64(base.B)*strength, 0, 255))), A: base.A,
		}
	})
	return faces
}

// PrepareRockAppearance binds final lighting without fitting decorations or changing
// the published shape snapshot. Renderers reuse this immutable CPU result.
func PrepareRockAppearance(topology *RockTopology) *RockTopology {
	if topology.Appearance != nil {
		return topology
	}
	t := *topology
	t.Appearance = ShadeRockFaces(t.Grid, t.Boundary, t.neighbors)
	return &t
}
