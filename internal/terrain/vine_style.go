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

// Authored families share a material evaluated at distances along their stems.
// Generated vines instead sample the shared vegetation field in world space.
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

// VineGreenAt keeps generated stems and leaf eligibility on the same field.
// Explicitly authored styles retain their distance-based transitions.
func VineGreenAt(vine Vine, p geom.V, distance float64) float64 {
	if vine.styleSet {
		return VegetationGreen(WorldPoint(vine.materialOrientation, p).Add(vine.materialOrigin))
	}
	return vine.Style.Green(distance)
}

func decorateVines(field *VineTerrain, vines []Vine) {
	for i := range vines {
		v := &vines[i]
		if len(v.Points) == 0 {
			continue
		}
		// A separate geometry-derived RNG never consumes growth randomness.
		root := v.Points[0].P
		v.materialOrientation = field.orientation
		v.materialOrigin = WorldPoint(field.orientation, geom.V{Y: field.worldTop})
		worldRoot := WorldPoint(v.materialOrientation, root).Add(v.materialOrigin)
		seed := SectionSeed(int64(math.Round(worldRoot.X*1e7)), int64(math.Round(worldRoot.Y*1e7)))
		rng := rand.New(rand.NewSource(seed))
		v.Along = make([]float64, len(v.Points))
		if v.Parent >= 0 && v.Parent < i {
			v.Style = vines[v.Parent].Style
			v.Along[0] = vines[v.Parent].Along[v.Joint]
		} else {
			hue := VineSage
			if VegetationGreen(worldRoot) < .5 {
				hue = VineStraw
			}
			v.Style = VineStyle{From: hue, To: hue, Tint: geom.Lerp(-.045, .045, rng.Float64())}
		}
		for j := 1; j < len(v.Points); j++ {
			v.Along[j] = v.Along[j-1] + v.Points[j].P.Sub(v.Points[j-1].P).Len()
		}
		v.styleFamily, v.styleSet = VineFamily(vines, i), true
		v.Leaves = generateVineLeaves(field, *v, rng)
	}
}

// Polygon follows the attachment, left shoulder, pointed tip, right shoulder.
// Along associates the leaf with its original stem through repeated cuts.
type VineLeaf struct {
	Anchor  geom.V
	Along   float64
	Polygon [4]geom.V
}

func generateVineLeaves(field *VineTerrain, vine Vine, rng *rand.Rand) []VineLeaf {
	if vine.EdgeAligned || len(vine.Points) < 3 {
		return nil
	}
	var leaves []VineLeaf
	next := vine.Along[0] + geom.Lerp(.025, .055, rng.Float64())
	side := 1.0
	if rng.Intn(2) == 0 {
		side = -1
	}
	for i := 1; i < len(vine.Points)-1; i++ {
		if vine.Along[i] < next {
			continue
		}
		next = vine.Along[i] + geom.Lerp(.045, .085, rng.Float64())
		side = -side
		p := vine.Points[i]
		green := VineGreenAt(vine, p.P, vine.Along[i])
		if p.Radius < .0007 || green < .5 || rng.Float64() > green*.85 {
			continue
		}
		tangent := vine.Points[i+1].P.Sub(vine.Points[i-1].P).Norm()
		length := geom.Clamp(p.Radius*geom.Lerp(9, 13, rng.Float64()), .014, .035)
		if field.onForeground && p.Radius <= .0015 {
			length *= 1.3
		}
		// Angular, unequal halves echo the faceted rock and reference leaves.
		direction := geom.Rotate(tangent, side*geom.Lerp(.65, 1.25, rng.Float64()))
		base := p.P.Add(tangent.Perp().Mul(side * p.Radius * .7))
		tip := base.Add(direction.Mul(length))
		shoulder := base.Add(direction.Mul(length * .45))
		normal := direction.Perp().Mul(length * .24)
		leaf := VineLeaf{Anchor: p.P, Along: vine.Along[i], Polygon: [4]geom.V{base, shoulder.Add(normal), tip, shoulder.Sub(normal.Mul(.75))}}
		if vineLeafFits(field, leaf) {
			leaves = append(leaves, leaf)
		}
	}
	return leaves
}

func vineLeafFits(field *VineTerrain, leaf VineLeaf) bool {
	// Sample the interior as well as edges: a narrow gap or crest exclusion
	// can pass through a leaf even if all four corners lie on supporting rock.
	for _, triangle := range [][3]geom.V{
		{leaf.Polygon[0], leaf.Polygon[1], leaf.Polygon[2]},
		{leaf.Polygon[0], leaf.Polygon[2], leaf.Polygon[3]},
	} {
		steps := max(1, int(math.Ceil(max(triangle[1].Sub(triangle[0]).Len(), triangle[2].Sub(triangle[0]).Len())/(vineFieldStep*.5))))
		for i := 0; i <= steps; i++ {
			for j := 0; j <= steps-i; j++ {
				p := triangle[0].Add(triangle[1].Sub(triangle[0]).Mul(float64(i) / float64(steps))).Add(triangle[2].Sub(triangle[0]).Mul(float64(j) / float64(steps)))
				if field.growthSpace(p) < .0015 {
					return false
				}
			}
		}
	}
	return true
}
