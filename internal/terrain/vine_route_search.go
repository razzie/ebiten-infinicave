package terrain

import (
	"math/rand"
	"sort"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

type vineRouteStep struct{ edge, end int }

// Try alternatives at dead ends before committing a stem. A bounded search
// keeps the broad growth direction while finding long paths around top cells.
func (r *vineRoutes) plan(root geom.V, edge, end int, guide []VinePoint, heading geom.V, reach, radius float64, rng *rand.Rand, allowance vineTopAllowance) []vineRouteStep {
	physicalRoutes := *r
	var physical pathSupport
	var topHits [][]int
	var references []int
	distinct := 0
	if r.support != nil && allowance.limit > 0 {
		physical = r.support.withoutTopCells()
		physicalRoutes.support = &physical
		topHits = make([][]int, len(r.graph.edges))
		references = make([]int, len(r.support.topCells))
		for _, cell := range allowance.cells {
			references[cell] = 1
			distinct++
		}
	}
	visited := make([]bool, len(r.graph.points))
	e := r.graph.edges[edge]
	for _, node := range []int{e.a, e.b} {
		if node != end && r.graph.points[node] == root {
			visited[node] = true
		}
	}
	valid := make([]int8, len(r.graph.edges))
	fits := func(edge int) bool {
		if valid[edge] == 0 {
			e := r.graph.edges[edge]
			valid[edge] = -1
			if physicalRoutes.fits(r.graph.points[e.a], r.graph.points[e.b], radius) {
				valid[edge] = 1
				if topHits != nil {
					topHits[edge] = r.support.topCellsTouched(r.graph.points[e.a], r.graph.points[e.b], radius)
				}
			}
		}
		return valid[edge] > 0
	}
	budget := 384
	if len(guide) == 0 {
		budget = 96
	}
	var path, best []vineRouteStep
	longest := 0.0
	var search func(geom.V, int, int, geom.V, float64, int) bool
	search = func(p geom.V, edge, end int, heading geom.V, length float64, target int) bool {
		if budget == 0 || len(path) >= 160 || visited[end] {
			return false
		}
		budget--
		q := r.graph.points[end]
		if len(path) == 0 && !physicalRoutes.fits(p, q, radius) {
			return false
		}
		if topHits != nil {
			hits := topHits[edge]
			if len(path) == 0 {
				hits = r.support.topCellsTouched(p, q, radius)
			}
			added := 0
			for _, cell := range hits {
				if references[cell] == 0 {
					added++
				}
				references[cell]++
			}
			defer func() {
				for _, cell := range hits {
					references[cell]--
				}
			}()
			if distinct+added > allowance.limit {
				return false
			}
			distinct += added
			defer func() { distinct -= added }()
		}
		length += q.Sub(p).Len()
		path = append(path, vineRouteStep{edge, end})
		visited[end] = true
		defer func() {
			visited[end] = false
			path = path[:len(path)-1]
		}()
		if length > longest {
			longest = length
			best = append(best[:0], path...)
		}
		if length >= reach {
			return true
		}
		if d := q.Sub(p); d.Len2() > 1e-18 {
			heading = d.Norm()
		}
		if len(guide) > 1 {
			for target < len(guide)-1 && (q.Sub(guide[target].P).Len() < .035 || q.Sub(guide[target+1].P).Len2() < q.Sub(guide[target].P).Len2()) {
				target++
			}
			d := guide[target].P.Sub(q)
			if target == len(guide)-1 && d.Len() < .05 {
				// Carry on in the guide's final direction instead of curling
				// back toward its endpoint when the requested reach is longer.
				d = guide[target].P.Sub(guide[max(0, target-3)].P)
			}
			if d.Len2() > 1e-8 {
				heading = d.Norm()
			}
		}
		type candidate struct {
			vineRouteStep
			score float64
		}
		var choices []candidate
		for _, next := range r.graph.links[end] {
			if next == edge || r.used[next] || !fits(next) {
				continue
			}
			e := r.graph.edges[next]
			node := e.a
			if node == end {
				node = e.b
			}
			if visited[node] {
				continue
			}
			d := r.graph.points[node].Sub(q)
			score := heading.Dot(d.Norm()) + rng.Float64()*.22
			if topHits != nil {
				for _, cell := range topHits[next] {
					if references[cell] == 0 {
						score -= .35
					}
				}
			}
			if len(guide) > 1 {
				score -= min(.10, r.graph.points[node].Sub(guide[target].P).Len()) * 4
			}
			choices = append(choices, candidate{vineRouteStep{next, node}, score})
		}
		sort.SliceStable(choices, func(i, j int) bool { return choices[i].score > choices[j].score })
		for _, c := range choices {
			if search(q, c.edge, c.end, heading, length, target) {
				return true
			}
		}
		return false
	}
	search(root, edge, end, heading, 0, min(1, len(guide)-1))
	return best
}
