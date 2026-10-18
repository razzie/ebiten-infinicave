package terrain

import (
	"image/color"
	"math"
	"math/rand"
	"sort"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/parallel"
)

// RockCell is shared by rendering and vine growth; colors belong to entire
// Voronoi faces, not a second approximation of the original noise field.
type RockCell struct {
	orientation     Orientation // Lighting basis for the internal section frame.
	worldTop        float64     // Stable material origin across padded windows.
	Center          geom.V
	Polygon         []geom.V
	Color           color.NRGBA
	Z               float64 // height toward the camera at the control point
	Normal          geom.V3
	Raised          bool
	Shadow, Ambient float64
}

type RockGrid []RockCell

func newRockGrid(seeds []geom.V, colorAt func(geom.V) color.NRGBA) RockGrid {
	return newRockGridInRange(seeds, colorAt, 0, generationWidth)
}

func newRockGridInRange(seeds []geom.V, colorAt func(geom.V) color.NRGBA, minX, maxX float64) RockGrid {
	cells := voronoiCellsInRange(seeds, minX, maxX)
	grid := make(RockGrid, 0, len(seeds))
	for i, p := range seeds {
		clr := colorAt(p)
		if clr.A != 0 {
			grid = append(grid, RockCell{Center: p, Polygon: cells[i], Color: clr})
		}
	}
	return grid
}

// Keep complete Voronoi cells in the padded footprint. The exposed cell edges
// make a broken, angular silhouette without clipping smooth curves through
// faces or adding detail to any shared interior edge.
func newBackgroundRockGrid(seed int64, seeds []geom.V, footprint *rockFootprint) RockGrid {
	cells := voronoiCellsInRange(seeds, BackgroundMinX, BackgroundMaxX)
	holes := backgroundPaddingHoles(seed, footprint.worldTop)
	grid := make(RockGrid, 0, len(seeds))
	for i, p := range seeds {
		keep := footprint.backgroundContains(p)
		for _, vertex := range cells[i] {
			if keep {
				break
			}
			keep = footprint.backgroundContains(vertex)
		}
		if keep && !footprint.backgroundHoleCell(p, cells[i], holes) {
			grid = append(grid, RockCell{Center: p, Polygon: cells[i], Color: color.NRGBA{A: 255}})
		}
	}
	return grid
}

func InsetForegroundGrid(grid RockGrid) RockGrid {
	clipped, _ := insetForegroundGrid(grid, false)
	return clipped
}

func insetForegroundGrid(grid RockGrid, mapping bool) (RockGrid, []int) {
	clipped := make(RockGrid, 0, len(grid))
	var parents []int
	if mapping {
		parents = make([]int, 0, len(grid))
	}
	for parent, cell := range grid {
		poly := geom.ClipHalfPlane(cell.Polygon, geom.V{X: -1, Y: 0}, -foregroundScreenInset)
		poly = geom.ClipHalfPlane(poly, geom.V{X: 1, Y: 0}, generationWidth-foregroundScreenInset)
		if len(poly) < 3 || geom.PolygonArea(poly) < 1e-15 {
			continue
		}
		cell.Polygon = poly
		if !geom.InsidePolygon(cell.Center, poly) {
			cell.Center = geom.PolygonCenter(poly)
		}
		clipped = append(clipped, cell)
		if mapping {
			parents = append(parents, parent)
		}
	}
	return clipped, parents
}

// RockOrientation returns the lighting basis of a generated rock face.
func RockOrientation(cell RockCell) Orientation { return cell.orientation }

// RockMaterialPoint supplies world coordinates for deterministic face detail.
func RockMaterialPoint(cell RockCell, p geom.V) geom.V {
	p.Y += cell.worldTop
	return WorldPoint(cell.orientation, p)
}

// WithRockOrientation selects a lighting basis for an internal diagnostic face.
func WithRockOrientation(cell RockCell, orientation Orientation) RockCell {
	cell.orientation = orientation
	return cell
}

// parallelFor shares the terrain and rendering helper budget.
func parallelFor(n int, fn func(i int)) { parallel.For(n, fn) }

type siteGrid struct {
	pts        []geom.V
	minX, maxX float64
	size       float64
	cols, rows int
	buckets    [][]int32
}

func newSiteGridInRange(pts []geom.V, minX, maxX float64) *siteGrid {
	g := &siteGrid{pts: pts, size: .024, minX: minX, maxX: maxX}
	g.cols, g.rows = int((maxX-minX)/g.size)+1, int(GenerationHeight/g.size)+1
	g.buckets = make([][]int32, g.cols*g.rows)
	for i, p := range pts {
		cx, cy := g.cellOf(p)
		g.buckets[cy*g.cols+cx] = append(g.buckets[cy*g.cols+cx], int32(i))
	}
	return g
}

func (g *siteGrid) cellOf(p geom.V) (int, int) {
	return min(max(int((p.X-g.minX)/g.size), 0), g.cols-1), min(max(int((p.Y-GenerationMinY)/g.size), 0), g.rows-1)
}

// A site farther than twice the cell's radius cannot cut it, so rings of
// buckets are clipped nearest first until the next ring is out of reach.
func (g *siteGrid) cell(i int) []geom.V {
	a := g.pts[i]
	poly := []geom.V{{X: g.minX, Y: GenerationMinY}, {X: g.maxX, Y: GenerationMinY}, {X: g.maxX, Y: generationMaxY}, {X: g.minX, Y: generationMaxY}}
	cx, cy := g.cellOf(a)
	var ring []int32
	reach2 := math.Inf(1)
	for r := 0; r < max(g.cols, g.rows); r++ {
		// Everything in ring r is at least (r-1)*size away from a.
		if gap := float64(r-1) * g.size; gap > 0 && gap*gap >= reach2 {
			break
		}
		ring = ring[:0]
		for y := cy - r; y <= cy+r; y++ {
			if y < 0 || y >= g.rows {
				continue
			}
			for x := cx - r; x <= cx+r; x++ {
				if x < 0 || x >= g.cols || (max(abs(x-cx), abs(y-cy)) != r) {
					continue
				}
				for _, j := range g.buckets[y*g.cols+x] {
					if int(j) != i {
						ring = append(ring, j)
					}
				}
			}
		}
		sort.Slice(ring, func(p, q int) bool {
			dp, dq := g.pts[ring[p]].Sub(a).Len2(), g.pts[ring[q]].Sub(a).Len2()
			return dp < dq || (dp == dq && ring[p] < ring[q])
		})
		for _, j := range ring {
			b := g.pts[j]
			poly = geom.ClipHalfPlane(poly, b.Sub(a), 0.5*(b.Len2()-a.Len2()))
			if len(poly) == 0 {
				return nil
			}
		}
		radius2 := 0.0
		for _, p := range poly {
			radius2 = math.Max(radius2, p.Sub(a).Len2())
		}
		reach2 = 4 * radius2
	}
	// Overlapping windows must round identically, so redo the clips in site
	// order over every site that could reach the cell.
	reach := math.Sqrt(reach2) + .001
	span := int(math.Ceil(reach / g.size))
	ring = ring[:0]
	for y := max(cy-span, 0); y <= min(cy+span, g.rows-1); y++ {
		for x := max(cx-span, 0); x <= min(cx+span, g.cols-1); x++ {
			for _, j := range g.buckets[y*g.cols+x] {
				if int(j) != i && g.pts[j].Sub(a).Len2() <= reach*reach {
					ring = append(ring, j)
				}
			}
		}
	}
	sort.Slice(ring, func(p, q int) bool { return ring[p] < ring[q] })
	poly = []geom.V{{X: g.minX, Y: GenerationMinY}, {X: g.maxX, Y: GenerationMinY}, {X: g.maxX, Y: generationMaxY}, {X: g.minX, Y: generationMaxY}}
	for _, j := range ring {
		b := g.pts[j]
		poly = geom.ClipHalfPlane(poly, b.Sub(a), 0.5*(b.Len2()-a.Len2()))
		if len(poly) == 0 {
			return nil
		}
	}
	return geom.OrderPolygon(poly)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// voronoiCells computes every cell in parallel; element i equals voronoiCell(i, pts).
func voronoiCells(pts []geom.V) [][]geom.V {
	return voronoiCellsInRange(pts, 0, generationWidth)
}

func voronoiCellsInRange(pts []geom.V, minX, maxX float64) [][]geom.V {
	grid := newSiteGridInRange(pts, minX, maxX)
	cells := make([][]geom.V, len(pts))
	parallelFor(len(pts), func(i int) { cells[i] = grid.cell(i) })
	return cells
}

func voronoiCell(i int, pts []geom.V) []geom.V {
	poly := []geom.V{{X: 0, Y: GenerationMinY}, {X: generationWidth, Y: GenerationMinY}, {X: generationWidth, Y: generationMaxY}, {X: 0, Y: generationMaxY}}
	a := pts[i]
	for j, b := range pts {
		if i == j {
			continue
		}
		n := b.Sub(a)
		c := 0.5 * (b.Len2() - a.Len2())
		poly = geom.ClipHalfPlane(poly, n, c)
		if len(poly) == 0 {
			break
		}
	}
	return geom.OrderPolygon(poly)
}

// Site density follows terrain only; guides cut faces without adding sites.
const maxRockSeedSpacing = .030

func desiredSpacing(p geom.V, noise *Perlin) float64 {
	return geom.Lerp(maxRockSeedSpacing, .022, geom.Smoothstep(.39, .58, fbm(noise, p)))
}

func generateSeeds(rng *rand.Rand, count int, noise *Perlin) []geom.V {
	// Weighted sampling follows background terrain.
	pts := make([]geom.V, 0, count)
	for len(pts) < count {
		best := geom.V{}
		bestD2 := -1.0
		candidates := 22
		if len(pts) < 8 {
			candidates = 8
		}
		for c := 0; c < candidates; c++ {
			p := geom.V{X: rng.Float64() * generationWidth, Y: GenerationMinY + rng.Float64()*GenerationHeight}
			minD2 := math.Inf(1)
			for _, q := range pts {
				d2 := p.Sub(q).Len2()
				if d2 < minD2 {
					minD2 = d2
				}
			}
			spacing := desiredSpacing(p, noise)
			minD2 /= spacing * spacing
			if minD2 > bestD2 {
				best, bestD2 = p, minD2
			}
		}
		pts = append(pts, best)
	}
	return pts
}

// Repel crowded seeds without forcing a regular lattice.
func relaxSeeds(seeds []geom.V, noise *Perlin) {
	spacing := make([]float64, len(seeds))
	for i, p := range seeds {
		spacing[i] = desiredSpacing(p, noise) * .48
	}
	for pass := 0; pass < 4; pass++ {
		shifts := make([]geom.V, len(seeds))
		for i, p := range seeds {
			for j := i + 1; j < len(seeds); j++ {
				d := p.Sub(seeds[j])
				distance := d.Len()
				minimum := (spacing[i] + spacing[j]) * .5
				if distance >= minimum {
					continue
				}
				push := d.Norm().Mul((minimum - distance) * .35)
				shifts[i] = shifts[i].Add(push)
				shifts[j] = shifts[j].Sub(push)
			}
		}
		for i := range seeds {
			seeds[i] = seeds[i].Add(shifts[i])
			seeds[i].X = geom.Clamp(seeds[i].X, .001, generationWidth-.001)
			seeds[i].Y = geom.Clamp(seeds[i].Y, GenerationMinY+.001, generationMaxY-.001)
		}
	}
}

func SectionSeed(seed, id int64) int64 {
	x := uint64(seed) + uint64(id)*0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return int64(x ^ (x >> 31))
}

func cellSeed(seed int64, p geom.V, top float64) int64 {
	return SectionSeed(SectionSeed(seed, int64(math.Round(p.X*1e6))), int64(math.Round((p.Y+top)*1e6)))
}

// Jittered world-space sites give neighboring generation windows exactly the
// same rocks in their overlap, independent of load order or cache eviction.
func worldSeeds(seed int64, top float64, noise *Perlin) []geom.V {
	return worldSeedsInRange(seed, top, noise, 0, generationWidth)
}

func worldSeedsInRange(seed int64, top float64, noise *Perlin, minX, maxX float64) []geom.V {
	const step = .022
	first := int64(math.Floor((top + GenerationMinY) / step))
	last := int64(math.Ceil((top + generationMaxY) / step))
	rows := make([][]geom.V, last-first)
	parallelFor(len(rows), func(n int) {
		row := first + int64(n)
		for col := int64(math.Floor(minX / step)); float64(col)*step < maxX; col++ {
			p := geom.V{X: (float64(col)+.5)*step + (SiteRandom(seed, row, col, 0)-.5)*step*.85,
				Y: (float64(row)+.5)*step + (SiteRandom(seed, row, col, 1)-.5)*step*.85 - top}
			if p.X <= minX+.001 || p.X >= maxX-.001 || p.Y <= GenerationMinY+.001 || p.Y >= generationMaxY-.001 {
				continue
			}
			spacing := desiredSpacing(p, noise)
			if SiteRandom(seed, row, col, 2) < min(1, step*step/(spacing*spacing)) {
				rows[n] = append(rows[n], p)
			}
		}
	})
	var seeds []geom.V
	for _, r := range rows {
		seeds = append(seeds, r...)
	}
	return seeds
}

// Three independent samples per world site need no stateful RNG or seed table.
// Coordinates and channel, rather than section/window order, choose the sample.
func SiteRandom(seed, row, col, channel int64) float64 {
	x := SectionSeed(SectionSeed(SectionSeed(seed, row), col), channel)
	return float64(uint64(x)>>11) * (1.0 / (1 << 53))
}
