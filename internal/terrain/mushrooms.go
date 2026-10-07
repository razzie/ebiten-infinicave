package terrain

import (
	"image/color"
	"math"
	"math/rand"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

type Mushroom struct {
	orientation   Orientation // Cap and growth directions in the section frame.
	Stem          []geom.V
	Anchor        geom.V
	RootDirection geom.V
	CapCenter     geom.V
	CapWidth      float64
	CapHeight     float64
	Color         color.NRGBA
}

type MushroomGroup struct {
	Mushrooms []Mushroom
}

const MushroomSink = .004

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

func MushroomsForGuides(guides []Guide, foreground RockGrid, orientation ...Orientation) []MushroomGroup {
	mode := OptionalOrientation(orientation)
	up := InternalPoint(mode, geom.V{X: 0, Y: -1})
	ground := mushroomGroundFromCells(foreground)
	var groups []MushroomGroup
	for i := range guides {
		g := &guides[i]
		if len(g.Pts) < 2 || len(g.S) != len(g.Pts) || g.S[len(g.S)-1] < .090 {
			continue
		}
		seed := g.Seed
		if seed == 0 {
			seed = SectionSeed(int64(math.Round(g.Pts[0].X*1e6)), int64(math.Round(g.Pts[0].Y*1e6)))
		}
		rng := rand.New(rand.NewSource(SectionSeed(seed, 0x6d757368)))
		length := g.S[len(g.S)-1]
		for s := geom.Lerp(.034, .074, rng.Float64()); s < length-.032; s += geom.Lerp(.155, .235, rng.Float64()) {
			count := 3 + rng.Intn(3)
			group := MushroomGroup{Mushrooms: make([]Mushroom, 0, count)}
			for j := 0; j < count; j++ {
				along := s + geom.Lerp(-.031, .031, rng.Float64())
				if along < .018 || along > length-.018 {
					continue
				}
				anchor, _, rockNormal := g.frameAt(along)
				if anchor.X < foregroundScreenInset || anchor.X > generationWidth-foregroundScreenInset || !mushroomOnGround(anchor, ground) {
					continue
				}
				air := rockNormal.Mul(-1)
				worldAir := WorldPoint(mode, air)
				if worldAir.Y > 0 {
					continue
				}
				size := geom.Lerp(.65, 1.2, rng.Float64())
				stemLength := geom.Lerp(.013, .023, rng.Float64()) * size
				capWidth := geom.Lerp(.009, .015, rng.Float64()) * size
				capHeight := capWidth * geom.Lerp(.38, .53, rng.Float64())
				// The root is buried in the rock; the foreground is drawn over it.
				root := anchor.Sub(air.Mul(MushroomSink))
				stemEnd := anchor.Add(air.Mul(stemLength * .38)).Add(up.Mul(stemLength * .72))
				capBack := capWidth*.5*math.Abs(worldAir.X) + capHeight*(1.18*math.Max(worldAir.Y, 0)+.02*math.Max(-worldAir.Y, 0))
				requiredAir := capBack + .0005
				requiredAir = math.Max(requiredAir, stemLength*.24*math.Max(-worldAir.Y, 0)+.0005)
				if distance := stemEnd.Sub(anchor).Dot(air); distance < requiredAir {
					stemEnd = stemEnd.Add(air.Mul(requiredAir - distance))
				}
				control1 := root.Add(air.Mul(stemLength*.46 + MushroomSink))
				control2 := stemEnd.Sub(up.Mul(stemLength * .24))
				stem := make([]geom.V, 9)
				for k := range stem {
					stem[k] = cubicBezier(root, control1, control2, stemEnd, float64(k)/float64(len(stem)-1))
				}
				mushroom := Mushroom{
					orientation: mode,
					Stem:        stem, Anchor: anchor, RootDirection: air,
					CapCenter: stemEnd.Add(up.Mul(capHeight * .18)),
					CapWidth:  capWidth, CapHeight: capHeight,
					Color: MushroomColors[rng.Intn(len(MushroomColors))],
				}
				if MushroomWithinForegroundInset(mushroom) {
					group.Mushrooms = append(group.Mushrooms, mushroom)
				}
			}
			if len(group.Mushrooms) >= 3 {
				groups = append(groups, group)
			}
		}
	}
	return groups
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
			outline = append(outline, m.CapCenter.Add(InternalPoint(m.orientation, p)))
		}
	}
	return outline
}

// MushroomOrientation returns the growth basis of a generated mushroom.
func MushroomOrientation(m Mushroom) Orientation { return m.orientation }
