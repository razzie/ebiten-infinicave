package terrain

import (
	"math"
	"sort"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/parallel"
)

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
