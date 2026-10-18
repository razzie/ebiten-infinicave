package terrain

import (
	"math"
	"math/rand"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// Spread stratified roots across the side margins, including the outer fade.
// Their broad curves use a fast path away from rear holes; inward runners
// also search the narrow band around exposed foreground edges.
func backgroundPaddingVines(seed, id int64, footprint *rockFootprint) []Vine {
	rng := rand.New(rand.NewSource(SectionSeed(seed^0x76696e6573, id)))
	vines := make([]Vine, 0, 96)
	addFamily := func(root, heading geom.V, inward float64, radius, reach float64) {
		phase := rng.Float64() * 2 * math.Pi
		stem := freePathInPadding(root, heading, reach, radius, phase, -1, 0, footprint)
		if length, _ := stem.extent(); len(stem.Points) < 5 || length < .10 {
			return
		}
		parent := len(vines)
		vines = append(vines, stem)
		for fork := 0; fork < 4; fork++ {
			joint := len(stem.Points) * (fork + 2) / 8
			if joint < 1 || joint >= len(stem.Points)-1 {
				continue
			}
			base := stem.Points[joint]
			d := stem.Points[joint+1].P.Sub(stem.Points[joint-1].P).Norm()
			heading := geom.Rotate(d, float64(2*(fork%2)-1)*.8)
			reach := .20 + .18*rng.Float64()
			if fork >= 2 {
				// Long connectors follow the padding toward the cave.
				// Once there, the local edge search guides them around rock.
				heading = geom.V{X: inward, Y: -.35 + .7*rng.Float64()}.Norm()
				padding := max(0, -base.P.X, base.P.X-generationWidth)
				reach = min(.90, padding+.38+.20*rng.Float64())
			}
			branch := freePathInPadding(base.P, heading, reach, min(.0028, base.Radius*.75), phase+float64(fork), parent, joint, footprint)
			branch.Depth = 1
			if length, _ := branch.extent(); len(branch.Points) >= 4 && length >= .045 {
				vines = append(vines, branch)
			}
		}
	}

	// Six staggered roots per side retain about one third of the former
	// density while spreading families across the full width of the padding.
	lanes := [...]float64{-.43, -.33, -.23, -.13, -.035, .065}
	for lane, x := range lanes {
		for side := 0; side < 2; side++ {
			sign := float64(1 - 2*side)
			root := geom.V{X: x + (rng.Float64()-.5)*.025, Y: .20 + .48*float64(lane%2) + (rng.Float64()-.5)*.16}
			if side != 0 {
				root.X = generationWidth - root.X
			}
			radius := .0032 + .001*rng.Float64()
			if !footprint.backgroundFits(root, root, radius) {
				// Roots can meet rear holes or inner-border rock. Keep local
				// retries in the side strip; never seed an isolated middle vine.
				origin, found := root, false
				for trial := 0; trial < 16; trial++ {
					p := origin.Add(geom.V{X: (rng.Float64() - .5) * .08, Y: (rng.Float64() - .5) * .20})
					inSide := p.X <= backgroundSolidBorderWidth || p.X >= generationWidth-backgroundSolidBorderWidth
					if inSide && p.Y > .02 && p.Y < SectionHeight-.02 && footprint.backgroundFits(p, p, radius) {
						root, found = p, true
						break
					}
				}
				if !found {
					continue
				}
			}
			drift := .08
			if lane >= len(lanes)-2 {
				drift = .4
			}
			addFamily(root, (geom.V{X: sign * drift, Y: -1}).Norm(), sign, radius, .75+.35*rng.Float64())
		}
	}

	// One bounded twig per successful connector supplies fine growth
	// without running the general occupancy search over every padding stem.
	parents := len(vines)
	for parent := 0; parent < parents; parent++ {
		stem := vines[parent]
		if stem.Depth != 1 || len(stem.Points) < 8 || rng.Float64() >= .7 {
			continue
		}
		joint := len(stem.Points) * 3 / 5
		base := stem.Points[joint]
		d := stem.Points[joint+1].P.Sub(stem.Points[joint-1].P).Norm()
		twig := freePathInPadding(base.P, geom.Rotate(d, (rng.Float64()-.5)*1.6), .08+.10*rng.Float64(), min(.0013, base.Radius*.6), rng.Float64()*2*math.Pi, parent, joint, footprint)
		twig.Depth = 2
		if length, _ := twig.extent(); len(twig.Points) >= 3 && length >= .035 {
			vines = append(vines, twig)
		}
	}
	decoratePathVines(vines, nil, SectionSeed(seed, id), footprint)
	return vines
}
