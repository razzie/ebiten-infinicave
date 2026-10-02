package main

import (
	"math"
	"runtime"
	"sort"
	"sync"
)

// parallelFor runs fn over [0,n) in contiguous chunks. fn must only write
// state owned by its index so results never depend on scheduling.
func parallelFor(n int, fn func(i int)) {
	workers := min(runtime.GOMAXPROCS(0), n)
	if workers <= 1 {
		for i := 0; i < n; i++ {
			fn(i)
		}
		return
	}
	const chunk = 8
	var next sync.Mutex
	start := 0
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				next.Lock()
				lo := start
				start += chunk
				next.Unlock()
				if lo >= n {
					return
				}
				for i := lo; i < min(lo+chunk, n); i++ {
					fn(i)
				}
			}
		}()
	}
	wg.Wait()
}

type siteGrid struct {
	pts        []V
	size       float64
	cols, rows int
	buckets    [][]int32
}

func newSiteGrid(pts []V) *siteGrid {
	g := &siteGrid{pts: pts, size: 24}
	g.cols, g.rows = int(W/g.size)+1, int(H/g.size)+1
	g.buckets = make([][]int32, g.cols*g.rows)
	for i, p := range pts {
		cx, cy := g.cellOf(p)
		g.buckets[cy*g.cols+cx] = append(g.buckets[cy*g.cols+cx], int32(i))
	}
	return g
}

func (g *siteGrid) cellOf(p V) (int, int) {
	return min(max(int(p.X/g.size), 0), g.cols-1), min(max(int(p.Y/g.size), 0), g.rows-1)
}

// A site farther than twice the cell's radius cannot cut it, so rings of
// buckets are clipped nearest first until the next ring is out of reach.
func (g *siteGrid) cell(i int) []V {
	a := g.pts[i]
	poly := []V{{0, 0}, {W, 0}, {W, H}, {0, H}}
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
			poly = clipHalfPlane(poly, b.Sub(a), 0.5*(b.Len2()-a.Len2()))
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
	reach := math.Sqrt(reach2) + 1
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
	poly = []V{{0, 0}, {W, 0}, {W, H}, {0, H}}
	for _, j := range ring {
		b := g.pts[j]
		poly = clipHalfPlane(poly, b.Sub(a), 0.5*(b.Len2()-a.Len2()))
		if len(poly) == 0 {
			return nil
		}
	}
	return orderPolygon(poly)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// voronoiCells computes every cell in parallel; element i equals voronoiCell(i, pts).
func voronoiCells(pts []V) [][]V {
	grid := newSiteGrid(pts)
	cells := make([][]V, len(pts))
	parallelFor(len(pts), func(i int) { cells[i] = grid.cell(i) })
	return cells
}
