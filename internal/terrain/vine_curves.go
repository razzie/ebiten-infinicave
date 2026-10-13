package terrain

import (
	"math"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// Round graph turns within a narrow corridor. Preserve sample identities for
// fork attachments; whole-segment checks keep rounded ribbons on their rock.
func smoothVineRoutes(vines []Vine, support *pathSupport, pins [][]int) []Vine {
	for i := range vines {
		v := &vines[i]
		growth := support
		if support != nil {
			field := support.forVine(*v)
			growth = &field
		}
		fixed := make([]bool, len(v.Points))
		for _, joint := range pins[i] {
			fixed[joint] = true
		}
		v.EdgeAligned = false
		if v.Parent >= 0 {
			v.Points[0] = vines[v.Parent].Points[v.Joint]
			v.Points[0].Radius *= .60
		}
		for sweep := 0; sweep < 3; sweep++ {
			previous := append([]VinePoint(nil), v.Points...)
			for j := 1; j < len(previous)-1; j++ {
				if fixed[j] {
					continue
				}
				a, b, c := previous[j-1], previous[j], previous[j+1]
				target := a.P.Add(c.P).Mul(.25).Add(b.P.Mul(.5))
				d := target.Sub(b.P)
				if d.Len() > .003 {
					target = b.P.Add(d.Norm().Mul(.003))
				}
				radius := math.Max(a.Radius, math.Max(b.Radius, c.Radius))
				if growth != nil && (!growth.fits(v.Points[j-1].P, target, radius) || !growth.fits(target, c.P, radius)) {
					continue
				}
				// A child must be able to reach its rounded parent junction.
				clear := true
				for k := i + 1; k < len(vines); k++ {
					child := vines[k]
					if child.Parent == i && child.Joint == j && len(child.Points) > 1 && growth != nil && !growth.fits(target, child.Points[1].P, radius) {
						clear = false
						break
					}
				}
				if clear {
					v.Points[j].P = target
				}
			}
		}
		old := v.Points
		points := []VinePoint{old[0]}
		remap := make([]int, len(old))
		for j := 1; j < len(old); j++ {
			a, b := old[j-1], old[j]
			c1 := a.P.Add(b.P.Sub(old[max(0, j-2)].P).Mul(1.0 / 6))
			c2 := b.P.Sub(old[min(len(old)-1, j+1)].P.Sub(a.P).Mul(1.0 / 6))
			var span []VinePoint
			if curveSamples([4]geom.V{a.P, c1, c2, b.P}, a.Radius, b.Radius, growth, 0, &span) {
				points = append(points, span...)
			} else {
				points = append(points, b)
			}
			remap[j] = len(points) - 1
		}
		v.Points = points
		for k := i + 1; k < len(vines); k++ {
			if vines[k].Parent == i {
				vines[k].Joint = remap[vines[k].Joint]
			}
		}
		v.Points[len(v.Points)-1].Radius = 0
	}
	return vines
}

// The control hull bounds the cubic's deviation from a chord. Subdivide to
// <= .45 pixels at 1280 pixels/unit and check every emitted ribbon segment.
func curveSamples(p [4]geom.V, r0, r1 float64, support *pathSupport, depth int, out *[]VinePoint) bool {
	const errorBound = .00035
	flat := geom.SegmentDistanceSquared(p[1], p[1], p[0], p[3]) <= errorBound*errorBound && geom.SegmentDistanceSquared(p[2], p[2], p[0], p[3]) <= errorBound*errorBound
	if flat || depth == 8 {
		if support != nil && !support.fits(p[0], p[3], max(r0, r1)) {
			return false
		}
		*out = append(*out, VinePoint{P: p[3], Radius: r1})
		return true
	}
	a, b, c := geom.LerpVector(p[0], p[1], .5), geom.LerpVector(p[1], p[2], .5), geom.LerpVector(p[2], p[3], .5)
	d, e := geom.LerpVector(a, b, .5), geom.LerpVector(b, c, .5)
	m := geom.LerpVector(d, e, .5)
	radius := (r0 + r1) * .5
	return curveSamples([4]geom.V{p[0], a, d, m}, r0, radius, support, depth+1, out) && curveSamples([4]geom.V{m, e, c, p[3]}, radius, r1, support, depth+1, out)
}
