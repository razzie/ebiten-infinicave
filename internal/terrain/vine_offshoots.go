package terrain

import (
	"math"
	"math/rand"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// Add short curved growth between graph junctions as well as at them. Each
// offshoot keeps an exact parent sample and stops before leaving its support.
func addFreeVineBranches(vines []Vine, support *pathSupport, rng *rand.Rand) []Vine {
	occupied := make(vineTwigOccupancy)
	for _, v := range vines {
		occupied.add(v)
	}
	parents := len(vines)
	for parent := 0; parent < parents; parent++ {
		stem := vines[parent]
		if stem.Route == GuideVineRoute {
			continue
		}
		growth := support
		if support != nil {
			field := support.forVine(stem)
			growth = &field
		}
		routes := &vineRoutes{support: growth}
		along, next := 0.0, .025+.025*rng.Float64()
		branches := 0
		side := 1.0
		if rng.Intn(2) == 0 {
			side = -1
		}
		for joint := 1; joint < len(stem.Points)-2 && branches < 4; joint++ {
			along += stem.Points[joint].P.Sub(stem.Points[joint-1].P).Len()
			if along < next {
				continue
			}
			next = along + .035 + .035*rng.Float64()
			base := stem.Points[joint]
			if base.Radius < .0005 {
				continue
			}
			tangent := stem.Points[joint+1].P.Sub(stem.Points[joint-1].P).Norm()
			for trial := 0; trial < 3; trial++ {
				heading := geom.Rotate(tangent, side*(.65+.65*rng.Float64()))
				reach := .030 + .055*rng.Float64()
				radius := min(.0012, base.Radius*.55)
				v := freePath(base.P, heading, reach, radius, rng.Float64()*2*math.Pi, parent, joint)
				v.Depth, v.Foreground = stem.Depth+1, stem.Foreground
				v.topCellLimit, v.allowedTopCells = stem.topCellLimit, stem.allowedTopCells
				for j := 1; j < len(v.Points); j++ {
					a, b := v.Points[j-1], v.Points[j]
					clear := routes.fits(a.P, b.P, max(a.Radius, b.Radius))
					// Allow the connector to leave the parent, then reserve space
					// from every existing stem, including earlier offshoots.
					for k := 1; clear && k <= 4; k++ {
						p := geom.LerpVector(a.P, b.P, float64(k)/4)
						if p.Sub(base.P).Len() > .012 && occupied.clearance(p) < max(a.Radius, b.Radius)+.002 {
							clear = false
						}
					}
					if !clear {
						v.Points = v.Points[:j]
						break
					}
				}
				if length, span := v.extent(); length < .022 || span < .016 {
					continue
				}
				v.Points[len(v.Points)-1].Radius = 0
				vines = append(vines, v)
				occupied.add(v)
				branches++
				break
			}
			side = -side
		}
	}
	return vines
}
