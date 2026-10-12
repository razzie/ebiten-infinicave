package terrain

import (
	"math"
	"math/rand"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

type VineHue uint8

const (
	VineSage VineHue = iota
	VineStraw
)

// A family shares one material, evaluated at distances along its stems.
// Zero transition width selects a single hue. Tint is a small brightness offset.
type VineStyle struct {
	From, To   VineHue
	Start, End float64
	// Period repeats the green-yellow path at this spacing.
	// Zero retains the single transition used by authored styles.
	Period float64
	Tint   float64
}

func (s VineStyle) Hues(distance float64) (VineHue, VineHue, float64) {
	start, end := s.Start, s.End
	from, to := s.From%2, s.To%2
	if end <= start || from == to {
		return from, from, 0
	}
	if s.Period > 0 && distance >= start {
		cycle := math.Floor((distance - start) / s.Period)
		if int(cycle)%2 != 0 {
			from, to = to, from
		}
		start, end = start+cycle*s.Period, end+cycle*s.Period
	}
	return from, to, geom.Smoothstep(start, end, distance)
}

func (s VineStyle) Blend(distance float64) float64 {
	_, _, blend := s.Hues(distance)
	return blend
}

func (s VineStyle) Green(distance float64) float64 {
	from, to, t := s.Hues(distance)
	green := 0.0
	if from == VineSage {
		green += 1 - t
	}
	if to == VineSage {
		green += t
	}
	return green
}

func VineFamily(vines []Vine, index int) int {
	family := index
	for vines[family].Parent >= 0 && vines[family].Parent < family {
		family = vines[family].Parent
	}
	return family
}

func VineStyleFamily(vines []Vine, index int) int {
	if vines[index].styleSet {
		return vines[index].styleFamily
	}
	return VineFamily(vines, index)
}

func VineMaterial(vines []Vine, index int) VineStyle {
	vine := vines[index]
	if vine.styleSet {
		return vine.Style
	}
	if style := vines[VineFamily(vines, index)].Style; style != (VineStyle{}) {
		return style
	}
	// Hand-authored vines also get a stable family palette.
	family := VineStyleFamily(vines, index)
	hue := VineHue(family % 2)
	return VineStyle{From: hue, To: hue, Tint: float64(family/2%6)*.02 - .05}
}

func VineDistance(vine Vine, point int) float64 {
	if len(vine.Along) == len(vine.Points) {
		return vine.Along[point]
	}
	along := 0.0
	for i := 1; i <= point; i++ {
		along += vine.Points[i].P.Sub(vine.Points[i-1].P).Len()
	}
	return along
}

func decorateVines(field *VineTerrain, vines []Vine) {
	for i := range vines {
		v := &vines[i]
		if len(v.Points) == 0 {
			continue
		}
		// A separate geometry-derived RNG never consumes growth randomness.
		root := v.Points[0].P
		seed := SectionSeed(int64(math.Round(root.X*1e7)), int64(math.Round(root.Y*1e7)))
		rng := rand.New(rand.NewSource(seed))
		v.Along = make([]float64, len(v.Points))
		if v.Parent >= 0 && v.Parent < i {
			v.Style = vines[v.Parent].Style
			v.Along[0] = vines[v.Parent].Along[v.Joint]
		} else {
			length, _ := v.extent()
			hue := VineHue(rng.Intn(2))
			v.Style = VineStyle{From: hue, To: hue, Tint: geom.Lerp(-.045, .045, rng.Float64())}
			if length >= .18 {
				v.Style.To = 1 - hue
				v.Style.Start = geom.Lerp(.025, .050, rng.Float64())
				v.Style.End = v.Style.Start + geom.Lerp(.055, .085, rng.Float64())
				v.Style.Period = geom.Lerp(.16, .22, rng.Float64())
			}
		}
		for j := 1; j < len(v.Points); j++ {
			v.Along[j] = v.Along[j-1] + v.Points[j].P.Sub(v.Points[j-1].P).Len()
		}
		v.styleFamily, v.styleSet = VineFamily(vines, i), true
		v.Leaves = generateVineLeaves(field, *v, rng)
	}
}
