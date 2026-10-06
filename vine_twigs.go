package infinicave

import (
	"math"
	"math/rand"
)

const vineMinEdgeBranchLength = .012

type vineEdge struct{ a, b int }

type vineEdgeGraph struct {
	points []V
	edges  []vineEdge
	links  [][]int
	bins   map[[2]int][]int
}

const vineEdgeBinSize = .032

func vineEdgeBin(p V) [2]int {
	return [2]int{int(math.Floor(p.X / vineEdgeBinSize)), int(math.Floor(p.Y / vineEdgeBinSize))}
}

func newVineEdgeGraph(background RockGrid) *vineEdgeGraph {
	g := &vineEdgeGraph{bins: make(map[[2]int][]int)}
	nodes := make(map[[2]int64]int)
	seen := make(map[[4]int64]bool)
	node := func(p V) int {
		key := [2]int64{int64(math.Round(p.X * 1e7)), int64(math.Round(p.Y * 1e7))}
		if i, ok := nodes[key]; ok {
			return i
		}
		i := len(g.points)
		nodes[key] = i
		g.points = append(g.points, p)
		g.links = append(g.links, nil)
		return i
	}
	for _, cell := range background {
		for j, a := range cell.Polygon {
			b := cell.Polygon[(j+1)%len(cell.Polygon)]
			if (a.X == b.X && (a.X == 0 || a.X == generationWidth)) || (a.Y == b.Y && (a.Y == generationMinY || a.Y == generationMaxY)) {
				continue
			}
			key := edgeKey(a, b)
			if seen[key] || b.Sub(a).Len2() < 1e-18 {
				continue
			}
			seen[key] = true
			e := vineEdge{node(a), node(b)}
			if e.a == e.b {
				continue
			}
			i := len(g.edges)
			g.edges = append(g.edges, e)
			g.links[e.a] = append(g.links[e.a], i)
			g.links[e.b] = append(g.links[e.b], i)
			lo := vineEdgeBin(V{math.Min(a.X, b.X), math.Min(a.Y, b.Y)})
			hi := vineEdgeBin(V{math.Max(a.X, b.X), math.Max(a.Y, b.Y)})
			for y := lo[1]; y <= hi[1]; y++ {
				for x := lo[0]; x <= hi[0]; x++ {
					g.bins[[2]int{x, y}] = append(g.bins[[2]int{x, y}], i)
				}
			}
		}
	}
	return g
}

// Keep branches off the existing ribbons, so increasing their count adds
// visible forks instead of repeatedly painting the same occupied seam.
type vineTwigOccupancy map[[2]int][]VinePoint

func (o vineTwigOccupancy) add(v Vine) {
	for _, p := range v.Points {
		key := vineEdgeBin(p.P)
		o[key] = append(o[key], p)
	}
}

func (o vineTwigOccupancy) clearance(p V) float64 {
	key := vineEdgeBin(p)
	distance := vineEdgeBinSize
	for y := key[1] - 1; y <= key[1]+1; y++ {
		for x := key[0] - 1; x <= key[0]+1; x++ {
			for _, q := range o[[2]int{x, y}] {
				distance = math.Min(distance, p.Sub(q.P).Len()-q.Radius)
			}
		}
	}
	return distance
}

func addVineEdgeBranches(field *VineTerrain, vines []Vine, rng *rand.Rand) []Vine {
	g := field.edges
	if g == nil || len(g.edges) == 0 {
		return vines
	}
	occupied := make(vineTwigOccupancy)
	for _, v := range vines {
		occupied.add(v)
	}
	used := make([]bool, len(g.edges))
	parents := len(vines)
	for parent := 0; parent < parents; parent++ {
		stem := vines[parent]
		// Sample every 12.5 pixels, staggered by seed. New twigs never become
		// independent roots or recursively fill every seam in the rock grid.
		for joint := 4 + rng.Intn(5); joint < len(stem.Points)-4; joint += 5 {
			base := stem.Points[joint]
			if base.Radius < .0007 {
				continue
			}
			bin := vineEdgeBin(base.P)
			radius := math.Min(.00095, base.Radius*lerp(.32, .46, rng.Float64()))
			reach := lerp(.022, .068, rng.Float64())
			best, bestScore := Vine{}, 0.0
			var bestEdges []int
			checked := make(map[int]bool)
			for y := bin[1] - 1; y <= bin[1]+1; y++ {
				for x := bin[0] - 1; x <= bin[0]+1; x++ {
					for _, edge := range g.bins[[2]int{x, y}] {
						if used[edge] || checked[edge] {
							continue
						}
						checked[edge] = true
						e := g.edges[edge]
						a, b := g.points[e.a], g.points[e.b]
						d := b.Sub(a)
						q := a.Add(d.Mul(clamp(base.P.Sub(a).Dot(d)/d.Len2(), 0, 1)))
						if q.Sub(base.P).Len() > .006 {
							continue
						}
						for _, end := range []int{e.a, e.b} {
							v, path, score := traceVineEdgeBranch(field, occupied, used, base.P, q, edge, end, radius, reach)
							if score > bestScore {
								best, bestEdges, bestScore = v, path, score
							}
						}
					}
				}
			}
			if bestScore == 0 {
				continue
			}
			best.Parent, best.Joint, best.Depth = parent, joint, stem.Depth+1
			vines = append(vines, best)
			occupied.add(best)
			for _, edge := range bestEdges {
				used[edge] = true
			}
			joint += 3 // Leave breathing room around each successful fork.
		}
	}
	// Keep every other completed offshoot. This halves their density while
	// preserving the seeded seam paths and spacing of the original growth.
	thinned := vines[:parents]
	for i := parents; i < len(vines); i += 2 {
		thinned = append(thinned, vines[i])
	}
	return thinned
}

func traceVineEdgeBranch(field *VineTerrain, occupied vineTwigOccupancy, used []bool, root, seam V, edge, end int, radius, reach float64) (Vine, []int, float64) {
	g := field.edges
	v := Vine{Points: []VinePoint{{root, radius}}, Parent: -1, EdgeAligned: true}
	length, exposed := 0.0, 0.0
	// The only connector goes from the exact parent point to its nearby seam.
	// All subsequent segments lie on the source polygon edges.
	advance := func(target V) bool {
		p := v.Points[len(v.Points)-1].P
		distance := target.Sub(p).Len()
		if distance < 1e-12 {
			return true
		}
		d := target.Sub(p).Mul(1 / distance)
		for distance > 1e-12 && length < reach {
			step := math.Min(.002, math.Min(distance, reach-length))
			q := p.Add(d.Mul(step))
			if field.growthSpace(q) < radius+.0005 || field.growthSpace(lerpV(p, q, .5)) < radius+.0005 {
				return false
			}
			length += step
			if length > .008 && occupied.clearance(q) > radius+.0005 {
				exposed += step
			}
			v.Points = append(v.Points, VinePoint{q, radius})
			p, distance = q, distance-step
		}
		return length < reach
	}
	var path []int
	if advance(seam) {
		for hops := 0; hops < 5; hops++ {
			path = append(path, edge)
			start := v.Points[len(v.Points)-1].P
			if !advance(g.points[end]) {
				break
			}
			heading := g.points[end].Sub(start).Norm()
			next, nextEnd, best := -1, 0, math.Inf(-1)
			for _, candidate := range g.links[end] {
				visited := used[candidate]
				for _, previous := range path {
					visited = visited || previous == candidate
				}
				if visited {
					continue
				}
				e := g.edges[candidate]
				other := e.a
				if other == end {
					other = e.b
				}
				d := g.points[other].Sub(g.points[end]).Norm()
				look := g.points[end].Add(d.Mul(math.Min(.008, g.points[other].Sub(g.points[end]).Len())))
				if field.growthSpace(look) < radius+.0005 {
					continue
				}
				score := occupied.clearance(look) + heading.Dot(d)*.002
				if score > best {
					next, nextEnd, best = candidate, other, score
				}
			}
			if next < 0 {
				break
			}
			edge, end = next, nextEnd
		}
	}
	if length < vineMinEdgeBranchLength || exposed < .008 || exposed < length*.35 {
		return Vine{}, nil, 0
	}
	along := 0.0
	for i := range v.Points {
		if i > 0 {
			along += v.Points[i].P.Sub(v.Points[i-1].P).Len()
		}
		v.Points[i].Radius = radius * smoothstep(0, math.Min(.018, length*.5), length-along)
	}
	v.Points[len(v.Points)-1].Radius = 0
	return v, path, exposed - .1*length
}
