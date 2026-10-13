package terrain

import (
	"math"
	"sync"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

const MushroomSampleCount = 256

var mushroomSamples struct {
	sync.Once
	groups [MushroomSampleCount]MushroomGroup
}

// MushroomSample returns an immutable canonical group for atlas preparation.
// Eight growth directions each contain 32 groups with mixed individual tilts.
func MushroomSample(index int) MushroomGroup {
	mushroomSamples.Do(func() {
		for i := range mushroomSamples.groups {
			angle := float64(i/32)*math.Pi/7 - math.Pi/2
			air := geom.V{X: math.Sin(angle), Y: -math.Cos(angle)}
			tangent := air.Perp()
			random := func(c int64) float64 { return SiteRandom(0x6d75736873616d70, int64(i), 0, c) }
			count := 3 + int(random(0)*3)
			g := MushroomGroup{}
			positions := make([]float64, count)
			for j := 1; j < count; j++ {
				positions[j] = positions[j-1] + .014 + .008*random(int64(j)*9+6)
			}
			for j := 0; j < count; j++ {
				root := tangent.Mul(positions[j] - positions[count-1]*.5)
				size := .65 + .55*random(int64(j)*9+7)
				length := (.013 + .010*random(int64(j)*9+1)) * size
				width := (.009 + .006*random(int64(j)*9+2)) * size
				height := width * (.40 + .10*random(int64(j)*9+3))
				end := root.Add(air.Mul(length * .50)).Add(geom.V{Y: -length * .80})
				capBack := width*.5*math.Abs(air.X) + height*(1.18*max(air.Y, 0)+.02*max(-air.Y, 0))
				if gap := end.Sub(root).Dot(air) - capBack; gap < .0015 {
					end = end.Add(air.Mul(.0015 - gap))
				}
				stem := make([]geom.V, 9)
				buried := root.Sub(air.Mul(MushroomSink))
				for k := range stem {
					stem[k] = cubicBezier(buried, root.Add(air.Mul(length*.48)), end.Add(geom.V{Y: length * .18}), end, float64(k)/float64(len(stem)-1))
				}
				stem[1] = buried.Add(air.Mul(.002))
				g.Mushrooms = append(g.Mushrooms, Mushroom{Stem: stem, Anchor: root, RootDirection: air, CapCenter: end.Add(geom.V{Y: -width * .075}), CapWidth: width, CapHeight: height, CapAngle: (random(int64(j)*9+4) - .5) * .65, Color: MushroomColors[int(random(int64(j)*9+5)*3)]})
			}
			mushroomSamples.groups[i] = g
		}
	})
	return mushroomSamples.groups[index%MushroomSampleCount]
}

// MushroomPlacement identifies a sampled group and its first member frame.
// Members have independent atlas transforms; hand-authored groups use meshes.
func MushroomPlacement(g MushroomGroup) (sample int, anchor geom.V, rotation, scale float64, ok bool) {
	return int(g.sample), g.anchor, g.rotation, g.scale, g.sampled
}

// MushroomSpriteSample is one immutable atlas plant rooted at the origin.
// Groups share the pages, but their members can fit a curved ledge independently.
func MushroomSpriteSample(index int) Mushroom {
	g := MushroomSample(index)
	m := g.Mushrooms[index%len(g.Mushrooms)]
	root := m.Anchor
	m.Anchor = geom.V{}
	m.Stem = append([]geom.V(nil), m.Stem...)
	for i := range m.Stem {
		m.Stem[i] = m.Stem[i].Sub(root)
		if d := -m.Stem[i].Dot(m.RootDirection); d > MushroomSink*.8 {
			m.Stem[i] = m.Stem[i].Add(m.RootDirection.Mul(d - MushroomSink*.8))
		}
	}
	m.CapCenter = m.CapCenter.Sub(root)
	return m
}

func MushroomSpritePlacement(m Mushroom) (sample int, anchor geom.V, rotation, scale float64, ok bool) {
	return int(m.spriteSample), m.Anchor, m.spriteRotation, m.spriteScale, m.sprite
}

func sampledMushroomsForGuides(guides []Guide, foreground RockGrid, density float64, mode Orientation) []MushroomGroup {
	if density <= 0 || len(foreground) == 0 {
		return nil
	}
	ground := mushroomGroundFromCells(foreground)
	var groups []MushroomGroup
	var placed []Mushroom
	for gi := range guides {
		g := &guides[gi]
		if len(g.Pts) < 2 || len(g.S) != len(g.Pts) {
			continue
		}
		seed := g.Seed
		if seed == 0 {
			seed = SectionSeed(int64(math.Round(g.Pts[0].X*1e6)), int64(math.Round(g.Pts[0].Y*1e6)))
		}
		length := g.S[len(g.S)-1]
		for slot, s := 0, .045+.040*SiteRandom(seed, 0, 0, 30); s < length-.040; slot, s = slot+1, s+.180+.090*SiteRandom(seed, int64(slot), 0, 31) {
			group := MushroomGroup{anchor: geom.V{}, sampled: true}
			count := 3 + int(SiteRandom(seed, int64(slot), 0, 3)*3)
			for trial := 0; trial < count*4 && len(group.Mushrooms) < count; trial++ {
				random := func(channel int64) float64 { return SiteRandom(seed, int64(slot), int64(trial), channel) }
				along := s + (random(4)-.5)*.085
				if along < .018 || along > length-.018 {
					continue
				}
				anchor, _, normal := g.frameAt(along)
				air := WorldPoint(mode, normal.Mul(-1))
				if air.Y > 1e-12 || !mushroomOnGround(anchor, ground) {
					continue
				}
				angle := math.Atan2(air.X, -air.Y)
				bin := max(0, min(7, int(math.Round((angle+math.Pi/2)*7/math.Pi))))
				sample := bin*32 + int(random(1)*32)
				rotation := angle - (float64(bin)*math.Pi/7 - math.Pi/2)
				scale := .75 + .40*random(2)
				m := MushroomSpriteSample(sample)
				m.orientation = mode
				m.Anchor = anchor
				transform := func(p geom.V) geom.V { return anchor.Add(InternalPoint(mode, geom.Rotate(p, rotation).Mul(scale))) }
				for k, p := range m.Stem {
					m.Stem[k] = transform(p)
				}
				m.CapCenter = transform(m.CapCenter)
				m.RootDirection = InternalPoint(mode, geom.Rotate(m.RootDirection, rotation))
				for _, cell := range ground {
					if anchor.X < cell.lo.X-.003 || anchor.X > cell.hi.X+.003 || anchor.Y < cell.lo.Y-.003 || anchor.Y > cell.hi.Y+.003 {
						continue
					}
					if support, ok := InsetRockAnchor(cell.poly, anchor, normal); ok {
						m.support, m.supportSet = support, true
						break
					}
				}
				if !m.supportSet {
					continue
				}

				m.CapWidth *= scale
				m.CapHeight *= scale
				m.CapAngle += rotation
				m.spriteSample, m.spriteRotation, m.spriteScale, m.sprite = uint16(sample), rotation, scale, true
				if !MushroomWithinForegroundInset(m) || mushroomsCrowded(m, placed) || mushroomsCrowded(m, group.Mushrooms) {
					continue
				}
				if len(group.Mushrooms) == 0 {
					group.anchor, group.sample, group.rotation, group.scale = anchor, uint16(sample), rotation, scale
				}
				group.Mushrooms = append(group.Mushrooms, m)
			}
			if len(group.Mushrooms) < 3 {
				continue
			}
			placed = append(placed, group.Mushrooms...)
			if SiteRandom(seed, int64(slot), 0, 0) < density {
				groups = append(groups, group)
			}
		}
	}
	return groups
}
