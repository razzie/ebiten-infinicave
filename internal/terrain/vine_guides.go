package terrain

import (
	"math"
	"math/rand"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

const (
	guideVineClearance = .005
	guideVineOffset    = .011
)

// A center stem can reach the solid side just below a nearby crest. Rare
// runners split in both directions to cover the whole supported guide.
func addGuideVineBranches(vines []Vine, support *pathSupport, rng *rand.Rand) []Vine {
	if support == nil || len(support.guidePaths) == 0 {
		return vines
	}
	used := make([]bool, len(support.guidePaths))
	parents := len(vines)
	for parent := 0; parent < parents; parent++ {
		stem := vines[parent]
		if stem.Route != CellCenterVineRoute || rng.Float64() >= .24 {
			continue
		}
		guided := support.forVine(stem).forRoute(GuideVineRoute)
		guide, joint, nearest := -1, -1, .16
		var projection Projection
		for j := max(1, len(stem.Points)/2); j < len(stem.Points)-1; j += 2 {
			if stem.Points[j].Radius < .0005 {
				continue
			}
			for gi := range support.guidePaths {
				g := &support.guidePaths[gi]
				if used[gi] || len(g.Pts) < 2 || len(g.S) != len(g.Pts) || g.S[len(g.S)-1] < .08 {
					continue
				}
				if pr, ok := g.projectWithin(stem.Points[j].P, nearest); ok && pr.Signed > 0 {
					guide, joint, nearest, projection = gi, j, pr.Dist, pr
				}
			}
		}
		if guide < 0 {
			continue
		}
		g := &support.guidePaths[guide]
		length := g.S[len(g.S)-1]
		tangent := stem.Points[joint+1].P.Sub(stem.Points[joint-1].P).Norm()
		direction := 1.0
		if tangent.Dot(projection.T) < 0 {
			direction = -1
		}
		full := rng.Float64() < .06
		phase := rng.Float64() * 2 * math.Pi
		for _, sign := range []float64{direction, -direction} {
			if sign != direction && !full {
				break
			}
			end := geom.Clamp(projection.S+sign*(.10+.28*rng.Float64()), 0, length)
			if full {
				end = 0
				if sign > 0 {
					end = length
				}
			}
			v := traceGuideVine(stem, joint, g, projection.S, end, phase, &guided)
			if len(v.Points) == 0 {
				continue
			}
			v.Parent = parent
			vines = append(vines, v)
			used[guide] = true
		}
	}
	return vines
}

func guideVinePoint(g *Guide, along, phase, radius, minimum float64, support *pathSupport) (geom.V, geom.V, bool) {
	p, tangent, normal := g.frameAt(along)
	const maxOffset = .14
	end := p.Add(normal.Mul(maxOffset))
	offset := max(minimum, guideVineOffset+.0015*math.Sin(along/.07+phase))
	// Ray/edge intersections move the runner below the nearest top-facing
	// band. Distant upward-facing patches do not pull it across intervening rock.
	type interval struct{ lo, hi float64 }
	var blocked []interval
	support.topFaces.Query(geom.Bounds{Min: geom.V{X: min(p.X, end.X), Y: min(p.Y, end.Y)}, Max: geom.V{X: max(p.X, end.X), Y: max(p.Y, end.Y)}}, func(i int) bool {
		if support.topCellAllowed(i) {
			return true
		}
		poly := support.topCells[i].Polygon
		lo, hi := math.Inf(1), math.Inf(-1)
		if geom.InsidePolygon(p, poly) {
			lo = 0
		}
		if geom.InsidePolygon(end, poly) {
			hi = maxOffset
		}
		for j, a := range poly {
			d := poly[(j+1)%len(poly)].Sub(a)
			cross := geom.Cross(normal, d)
			if math.Abs(cross) < 1e-12 {
				continue
			}
			t := geom.Cross(a.Sub(p), d) / cross
			u := geom.Cross(a.Sub(p), normal) / cross
			if t >= 0 && t <= maxOffset && u >= -1e-9 && u <= 1+1e-9 {
				lo, hi = min(lo, t), max(hi, t)
			}
		}
		if lo <= hi {
			blocked = append(blocked, interval{lo, hi})
		}
		return true
	})
	margin := radius + vineTopCellClearance + .002
	for pass := 0; pass <= len(blocked); pass++ {
		previous := offset
		for _, band := range blocked {
			if offset >= band.lo-margin && offset <= band.hi+margin {
				offset = max(offset, band.hi+margin)
			}
		}
		if offset == previous {
			break
		}
	}
	q := p.Add(normal.Mul(offset))
	routes := &vineRoutes{support: support}
	for offset <= maxOffset {
		q = p.Add(normal.Mul(offset))
		if routes.fits(q, q, radius) {
			return q, tangent, true
		}
		offset += .002
	}
	return q, tangent, false
}

func traceGuideVine(stem Vine, joint int, g *Guide, start, end, phase float64, support *pathSupport) Vine {
	reach := math.Abs(end - start)
	if reach < .045 {
		return Vine{}
	}
	sign := 1.0
	if end < start {
		sign = -1
	}
	base := stem.Points[joint]
	radius := min(.0010, base.Radius*.65)
	q, tangent, ok := guideVinePoint(g, start, phase, radius, 0, support)
	if !ok {
		return Vine{}
	}
	tangent = tangent.Mul(sign)
	heading := stem.Points[joint+1].P.Sub(stem.Points[joint-1].P).Norm()
	distance := q.Sub(base.P).Len()
	v := Vine{Route: GuideVineRoute, Foreground: stem.Foreground, Joint: joint, Depth: stem.Depth + 1, Points: []VinePoint{{P: base.P, Radius: radius}}}
	v.topCellLimit, v.allowedTopCells = stem.topCellLimit, stem.allowedTopCells
	controls := [4]geom.V{base.P, base.P.Add(heading.Mul(distance * .25)), q.Sub(tangent.Mul(distance * .30)), q}
	if !curveSamples(controls, radius, radius, support, 0, &v.Points) {
		return Vine{}
	}
	routes := &vineRoutes{support: support}
	for i := 1; i < len(v.Points); i++ {
		if !routes.fits(v.Points[i-1].P, v.Points[i].P, radius) {
			return Vine{}
		}
	}
	steps := max(1, int(math.Ceil(reach/.006)))
	traveled := 0.0
	crest, _, normal := g.frameAt(start)
	offset := q.Sub(crest).Dot(normal)
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		along := geom.Lerp(start, end, t)
		minimum := max(0, offset-.003)
		p, _, ok := guideVinePoint(g, along, phase, radius, minimum, support)
		a := v.Points[len(v.Points)-1]
		r := radius * math.Pow(1-t, .7)
		for attempt := 0; ok && !routes.fits(a.P, p, max(a.Radius, r)) && attempt < 12; attempt++ {
			crest, _, normal = g.frameAt(along)
			minimum = p.Sub(crest).Dot(normal) + .003
			p, _, ok = guideVinePoint(g, along, phase, radius, minimum, support)
		}
		if !ok || !routes.fits(a.P, p, max(a.Radius, r)) {
			break
		}
		crest, _, normal = g.frameAt(along)
		offset = p.Sub(crest).Dot(normal)
		v.Points = append(v.Points, VinePoint{P: p, Radius: r})
		traveled = reach * t
	}
	if traveled < .045 {
		return Vine{}
	}
	v.Points[len(v.Points)-1].Radius = 0
	return v
}
