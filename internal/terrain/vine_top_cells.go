package terrain

import (
	"slices"
	"sort"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

type vineTopAllowance struct {
	limit int
	cells []int
}

func (s pathSupport) topCellAllowed(cell int) bool {
	return slices.Contains(s.allowedTopCells, cell)
}

// Count full ribbons against distinct rendered cells, including narrow facets
// between samples and cells whose edges run parallel to a vine seam.
func (s pathSupport) visitTopCells(a, b geom.V, radius float64, visit func(int) bool) {
	pad := radius + vineTopCellClearance
	s.topFaces.Query(geom.Bounds{Min: geom.V{X: min(a.X, b.X) - pad, Y: min(a.Y, b.Y) - pad}, Max: geom.V{X: max(a.X, b.X) + pad, Y: max(a.Y, b.Y) + pad}}, func(i int) bool {
		poly := s.topCells[i].Polygon
		if geom.InsidePolygon(a, poly) || geom.InsidePolygon(b, poly) {
			return visit(i)
		}
		for j, p := range poly {
			if geom.SegmentDistanceSquared(a, b, p, poly[(j+1)%len(poly)]) < pad*pad {
				return visit(i)
			}
		}
		return true
	})
}

func (s pathSupport) topCellsTouched(a, b geom.V, radius float64) []int {
	var cells []int
	s.visitTopCells(a, b, radius, func(i int) bool {
		cells = append(cells, i)
		return true
	})
	return cells
}

func (s pathSupport) withoutTopCells() pathSupport {
	s.topCells, s.topFaces, s.allowedTopCells = nil, nil, nil
	return s
}

func (r *vineRoutes) fitsWithTopCells(a, b geom.V, radius float64, allowance vineTopAllowance) bool {
	if r.support == nil || allowance.limit == 0 {
		return r.fits(a, b, radius)
	}
	physical := r.support.withoutTopCells()
	routes := *r
	routes.support = &physical
	if !routes.fits(a, b, radius) {
		return false
	}
	count := len(allowance.cells)
	r.support.visitTopCells(a, b, radius, func(i int) bool {
		if !slices.Contains(allowance.cells, i) {
			count++
		}
		return count <= allowance.limit
	})
	return count <= allowance.limit
}

func (r *vineRoutes) cellsForPath(root geom.V, path []vineRouteStep, radius float64, allowance vineTopAllowance) []int {
	if r.support == nil || allowance.limit == 0 {
		return nil
	}
	cells := append([]int(nil), allowance.cells...)
	for _, step := range path {
		q := r.graph.points[step.end]
		r.support.visitTopCells(root, q, radius, func(i int) bool {
			if !slices.Contains(cells, i) {
				cells = append(cells, i)
			}
			return true
		})
		root = q
	}
	sort.Ints(cells)
	return cells
}
