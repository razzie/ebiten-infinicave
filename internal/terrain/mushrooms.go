package terrain

import (
	"image/color"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

type Mushroom struct {
	spriteSample                uint16
	spriteRotation, spriteScale float64
	sprite                      bool
	orientation                 Orientation // Cap and growth directions in the section frame.
	support                     geom.V
	supportSet                  bool
	Stem                        []geom.V
	Anchor                      geom.V // Visual root attachment; carving tests the inset rock support.
	RootDirection               geom.V
	CapCenter                   geom.V
	CapWidth                    float64
	CapHeight                   float64
	CapAngle                    float64 // radians of cap tilt in world axes
	Color                       color.NRGBA
}

type MushroomGroup struct {
	Mushrooms       []Mushroom
	sample          uint16
	anchor          geom.V
	rotation, scale float64
	sampled         bool
}

const MushroomSink = .004

// DefaultMushroomDensity is the eligible mushroom cluster occurrence probability (1 means all clusters are retained).
const DefaultMushroomDensity = 0.5

// StemWidth includes the dark border used when rendering the stalk.
func (m Mushroom) StemWidth() float64 { return m.CapWidth * .19 }

const mushroomStemGap = .0015

// Leave room for the caps as well as the stalks, including across nearby guides.
func mushroomsCrowded(m Mushroom, others []Mushroom) bool {
	for _, other := range others {
		clearance := (m.CapWidth+other.CapWidth)*.55 + .003
		if m.Anchor.Sub(other.Anchor).Len2() < clearance*clearance {
			return true
		}
	}
	return mushroomStemsOverlap(m, others)
}

type mushroomGround struct {
	poly []geom.V
	lo   geom.V
	hi   geom.V
}

var MushroomColors = [...]color.NRGBA{
	{239, 100, 30, 255},
	{250, 165, 40, 255},
	{249, 192, 70, 255},
}

func MushroomsForGuides(guides []Guide, foreground RockGrid, density float64, orientation ...Orientation) []MushroomGroup {
	return sampledMushroomsForGuides(guides, foreground, density, OptionalOrientation(orientation))
}

// Check the sampled curves, including their rendered borders, rather than
// just root spacing: stalks on bends can converge or cross above the guide.
func mushroomStemsOverlap(m Mushroom, others []Mushroom) bool {
	lo, hi := geom.PolygonBounds(m.Stem)
	for _, other := range others {
		clearance := (m.StemWidth()+other.StemWidth())*.5 + mushroomStemGap
		otherLo, otherHi := geom.PolygonBounds(other.Stem)
		if hi.X+clearance < otherLo.X || lo.X-clearance > otherHi.X ||
			hi.Y+clearance < otherLo.Y || lo.Y-clearance > otherHi.Y {
			continue
		}
		for i := 1; i < len(m.Stem); i++ {
			for j := 1; j < len(other.Stem); j++ {
				if geom.SegmentDistanceSquared(m.Stem[i-1], m.Stem[i], other.Stem[j-1], other.Stem[j]) < clearance*clearance {
					return true
				}
			}
		}
	}
	return false
}

func mushroomGroundFromCells(foreground RockGrid) []mushroomGround {
	ground := make([]mushroomGround, 0, len(foreground))
	for _, cell := range foreground {
		if len(cell.Polygon) < 3 {
			continue
		}
		bounds := makeGuideFragment(cell.Polygon, cell.Center, false)
		ground = append(ground, mushroomGround{cell.Polygon, bounds.lo, bounds.hi})
	}
	return ground
}

func mushroomOnGround(p geom.V, ground []mushroomGround) bool {
	for _, cell := range ground {
		if p.X < cell.lo.X-.002 || p.X > cell.hi.X+.002 || p.Y < cell.lo.Y-.002 || p.Y > cell.hi.Y+.002 {
			continue
		}
		for i, a := range cell.poly {
			d := cell.poly[(i+1)%len(cell.poly)].Sub(a)
			if d.Len2() == 0 {
				continue
			}
			q := a.Add(d.Mul(geom.Clamp(p.Sub(a).Dot(d)/d.Len2(), 0, 1)))
			if p.Sub(q).Len2() <= .000004 {
				return true
			}
		}
	}
	return false
}

func MushroomWithinForegroundInset(m Mushroom) bool {
	left, right := foregroundScreenInset, generationWidth-foregroundScreenInset
	for _, p := range MushroomCapOutline(m) {
		if p.X < left || p.X > right {
			return false
		}
	}
	for _, p := range m.Stem {
		if p.X < left || p.X > right {
			return false
		}
	}
	return true
}

func MushroomCapOutline(m Mushroom) []geom.V {
	const samples = 8
	x, y, half, h := 0.0, 0.0, m.CapWidth/2, m.CapHeight
	top := -h
	curves := [3][4]geom.V{
		{{X: x - half, Y: y}, {X: x - half*.92, Y: top + h*.12}, {X: x - half*.4, Y: top}, {X: x, Y: top}},
		{{X: x, Y: top}, {X: x + half*.4, Y: top}, {X: x + half*.92, Y: top + h*.12}, {X: x + half, Y: y}},
		{{X: x + half, Y: y}, {X: x + half*.68, Y: y + h*.2}, {X: x - half*.68, Y: y + h*.2}, {X: x - half, Y: y}},
	}
	var outline []geom.V
	for _, c := range curves {
		for k := 0; k < samples; k++ {
			p := cubicBezier(c[0], c[1], c[2], c[3], float64(k)/samples)
			outline = append(outline, m.CapCenter.Add(InternalPoint(m.orientation, geom.Rotate(p, m.CapAngle))))
		}
	}
	return outline
}

// Include the rendered dark border in the visual footprint, using the atlas
// scale and frame for generated plants. Carving only tests support anchors.
func MushroomCapFootprint(m Mushroom) []geom.V {
	outline := MushroomCapOutline(m)
	rotation, scale := 0.0, 1.0
	if m.sprite {
		rotation, scale = m.spriteRotation, m.spriteScale
	}
	center := m.CapCenter.Add(InternalPoint(m.orientation, geom.Rotate(geom.V{Y: -m.CapHeight * .4}, rotation)))
	for i, p := range outline {
		outline[i] = p.Add(p.Sub(center).Norm().Mul(.001 * scale))
	}
	return outline
}

// MushroomOrientation returns the growth basis of a generated mushroom.
func MushroomOrientation(m Mushroom) Orientation { return m.orientation }
