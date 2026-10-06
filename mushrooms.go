package infinicave

import (
	"image/color"
	"math"
	"math/rand"

	"github.com/hajimehoshi/ebiten/v2"
)

type Mushroom struct {
	Stem          []V
	Anchor        V
	RootDirection V
	CapCenter     V
	CapWidth      float64
	CapHeight     float64
	Color         color.NRGBA
}

type MushroomGroup struct {
	Mushrooms []Mushroom
}

const mushroomSink = 4.0

type mushroomGround struct {
	poly []V
	lo   V
	hi   V
}

var mushroomColors = [...]color.NRGBA{
	{132, 57, 42, 255},
	{171, 111, 58, 255},
	{160, 139, 91, 255},
}

func mushroomsForGuides(guides []Guide, foreground RockGrid) []MushroomGroup {
	ground := mushroomGroundFromCells(foreground)
	var groups []MushroomGroup
	for i := range guides {
		g := &guides[i]
		if len(g.Pts) < 2 || len(g.S) != len(g.Pts) || g.S[len(g.S)-1] < 90 {
			continue
		}
		seed := g.Seed
		if seed == 0 {
			seed = sectionSeed(int64(math.Round(g.Pts[0].X*1000)), int64(math.Round(g.Pts[0].Y*1000)))
		}
		rng := rand.New(rand.NewSource(sectionSeed(seed, 0x6d757368)))
		length := g.S[len(g.S)-1]
		for s := lerp(34, 74, rng.Float64()); s < length-32; s += lerp(155, 235, rng.Float64()) {
			count := 3 + rng.Intn(3)
			group := MushroomGroup{Mushrooms: make([]Mushroom, 0, count)}
			for j := 0; j < count; j++ {
				along := s + lerp(-31, 31, rng.Float64())
				if along < 18 || along > length-18 {
					continue
				}
				anchor, _, rockNormal := g.frameAt(along)
				if anchor.X < foregroundScreenInset || anchor.X > W-foregroundScreenInset || !mushroomOnGround(anchor, ground) {
					continue
				}
				air := rockNormal.Mul(-1)
				if air.Y > 0 {
					continue
				}
				size := lerp(.65, 1.2, rng.Float64())
				stemLength := lerp(13, 23, rng.Float64()) * size
				capWidth := lerp(9, 15, rng.Float64()) * size
				capHeight := capWidth * lerp(.38, .53, rng.Float64())
				// The root is buried in the rock; the foreground is drawn over it.
				root := anchor.Sub(air.Mul(mushroomSink))
				stemEnd := anchor.Add(air.Mul(stemLength * .38)).Add(V{0, -stemLength * .72})
				capBack := capWidth*.5*math.Abs(air.X) + capHeight*(1.18*math.Max(air.Y, 0)+.02*math.Max(-air.Y, 0))
				requiredAir := capBack + .5
				requiredAir = math.Max(requiredAir, stemLength*.24*math.Max(-air.Y, 0)+.5)
				if distance := stemEnd.Sub(anchor).Dot(air); distance < requiredAir {
					stemEnd = stemEnd.Add(air.Mul(requiredAir - distance))
				}
				control1 := root.Add(air.Mul(stemLength*.46 + mushroomSink))
				control2 := stemEnd.Add(V{0, stemLength * .24})
				stem := make([]V, 9)
				for k := range stem {
					stem[k] = cubicBezier(root, control1, control2, stemEnd, float64(k)/float64(len(stem)-1))
				}
				mushroom := Mushroom{
					Stem: stem, Anchor: anchor, RootDirection: air,
					CapCenter: V{stemEnd.X, stemEnd.Y - capHeight*.18},
					CapWidth:  capWidth, CapHeight: capHeight,
					Color: mushroomColors[rng.Intn(len(mushroomColors))],
				}
				if mushroomWithinForegroundInset(mushroom) {
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

func mushroomOnGround(p V, ground []mushroomGround) bool {
	for _, cell := range ground {
		if p.X < cell.lo.X-2 || p.X > cell.hi.X+2 || p.Y < cell.lo.Y-2 || p.Y > cell.hi.Y+2 {
			continue
		}
		for i, a := range cell.poly {
			d := cell.poly[(i+1)%len(cell.poly)].Sub(a)
			if d.Len2() == 0 {
				continue
			}
			q := a.Add(d.Mul(clamp(p.Sub(a).Dot(d)/d.Len2(), 0, 1)))
			if p.Sub(q).Len2() <= 4 {
				return true
			}
		}
	}
	return false
}

func mushroomWithinForegroundInset(m Mushroom) bool {
	left, right := foregroundScreenInset, W-foregroundScreenInset
	half := m.CapWidth / 2
	if m.CapCenter.X-half < left || m.CapCenter.X+half > right {
		return false
	}
	for _, p := range m.Stem {
		if p.X < left || p.X > right {
			return false
		}
	}
	return true
}

// One batched draw: per-mushroom vector paths allocated gigabytes per section.
func prepareMushrooms(groups []MushroomGroup) triangleMesh {
	var vertices []ebiten.Vertex
	var indices []uint32
	vertex := func(p V, c color.NRGBA) uint32 {
		a := float32(c.A) / 255
		vertices = append(vertices, ebiten.Vertex{DstX: float32(p.X), DstY: float32(p.Y), SrcX: .5, SrcY: .5,
			ColorR: float32(c.R) / 255 * a, ColorG: float32(c.G) / 255 * a, ColorB: float32(c.B) / 255 * a, ColorA: a})
		return uint32(len(vertices) - 1)
	}
	ribbon := func(points []V, width float64, c color.NRGBA) {
		var prev [2]uint32
		for i, p := range points {
			n := points[min(len(points)-1, i+1)].Sub(points[max(0, i-1)]).Norm().Perp().Mul(width / 2)
			cur := [2]uint32{vertex(p.Add(n), c), vertex(p.Sub(n), c)}
			if i > 0 {
				indices = append(indices, prev[0], prev[1], cur[1], prev[0], cur[1], cur[0])
			}
			prev = cur
		}
	}
	fan := func(center V, outline []V, grow float64, c color.NRGBA) {
		mid := vertex(center, c)
		first := uint32(len(vertices))
		for _, p := range outline {
			vertex(p.Add(p.Sub(center).Norm().Mul(grow)), c)
		}
		for i := range outline {
			indices = append(indices, mid, first+uint32(i), first+uint32((i+1)%len(outline)))
		}
	}
	for _, group := range groups {
		for _, m := range group.Mushrooms {
			ribbon(m.Stem, m.CapWidth*.19, color.NRGBA{R: 46, G: 37, B: 28, A: 255})
			ribbon(m.Stem, m.CapWidth*.11, color.NRGBA{R: 216, G: 190, B: 143, A: 255})
			outline := m.capOutline()
			center := V{m.CapCenter.X, m.CapCenter.Y - m.CapHeight*.4}
			fan(center, outline, 1, color.NRGBA{R: 48, G: 36, B: 27, A: 255})
			fan(center, outline, 0, m.Color)
		}
	}
	return triangleMesh{vertices: vertices, indices: indices}
}

func (m Mushroom) capOutline() []V {
	const samples = 8
	x, y, half, h := m.CapCenter.X, m.CapCenter.Y, m.CapWidth/2, m.CapHeight
	top := y - h
	curves := [3][4]V{
		{{x - half, y}, {x - half*.92, top + h*.12}, {x - half*.4, top}, {x, top}},
		{{x, top}, {x + half*.4, top}, {x + half*.92, top + h*.12}, {x + half, y}},
		{{x + half, y}, {x + half*.68, y + h*.2}, {x - half*.68, y + h*.2}, {x - half, y}},
	}
	var outline []V
	for _, c := range curves {
		for k := 0; k < samples; k++ {
			outline = append(outline, cubicBezier(c[0], c[1], c[2], c[3], float64(k)/samples))
		}
	}
	return outline
}
