package terrain

import (
	"math"
	"math/rand"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func generateBranchesWithStrategy(guides []Guide, noise *Perlin, rng *rand.Rand, strategy BranchingStrategy) []BranchSegment {
	shape := DefaultShapeOptions()
	shape.BranchingStrategy = strategy
	return generateBranchesWithOptions(guides, noise, rng, shape)
}

func generateBranchesWithOptions(guides []Guide, noise *Perlin, rng *rand.Rand, shape ShapeOptions) []BranchSegment {
	strategy := shape.BranchingStrategy
	if strategy == BranchingSimple {
		return generateBranches(guides, noise, rng, shape)
	}
	var result []BranchSegment
	for i := range guides {
		// Resolve each guide's tree independently. Window padding may add
		// distant guides; their branch acceptance must not reorder this tree.
		var branches []BranchSegment
		g := &guides[i]
		if g.Seed != 0 {
			rng = rand.New(rand.NewSource(g.Seed ^ 0x6272616e6368))
		}
		length := g.S[len(g.S)-1]
		if strategy == BranchingLoops {
			branches = appendLoops(branches, g, guides, noise, rng, shape)
			if len(branches) > 0 {
				result = append(result, branches...)
				continue
			}
		}
		count := max(1, int(math.Round(length/.18)))
		for j := 0; j < count; j++ {
			s := length * (float64(j) + geom.Lerp(.3, .7, rng.Float64())) / float64(count)
			q, tangent, normal := g.frameAt(s)
			root := q.Add(normal.Mul(.045))
			if reliefHeight(root, guides[i:i+1], noise, nil) <= rockContourHeight {
				continue
			}
			dir := normal.Add(tangent.Mul(geom.Lerp(-.45, .45, rng.Float64()))).Norm()
			branches = growRecursiveBranch(branches, root, dir, geom.Lerp(.32, .55, rng.Float64()), shape.branchWidth(rng.Float64()), 0, noise, guides, rng, shape)
		}
		result = append(result, branches...)
	}
	return result
}

// Grow the complete parent before its children. The final taper has a short
// capped tip; most of a long limb retains enough width for terrain sampling.
// Depth and root count bound both geometry and generation work.
func growRecursiveBranch(branches []BranchSegment, root, direction geom.V, reach, width float64, depth int, noise *Perlin, guides []Guide, rng *rand.Rand, shape ShapeOptions) []BranchSegment {
	const strength = .5
	if !branchCanGrow(root, strength, width, guides) {
		return branches
	}
	phase := rng.Float64() * 1000
	start := len(branches)
	p := root
	for d := 0.0; d < reach; {
		next := math.Min(d+.012, reach)
		u, v := d/reach, next/reach
		bend := noise.Noise(p.X*6+phase, (p.Y+noise.OffsetY)*6-phase)
		heading := direction.Add(direction.Perp().Mul(.8 * bend)).Norm()
		end := p.Add(heading.Mul(next - d))
		wa := width * (1 - .72*geom.Smoothstep(.72, 1, u))
		wb := width * (1 - .72*geom.Smoothstep(.72, 1, v))
		if !branchCanGrow(end, strength, wb, guides) || !branchCanGrow(geom.LerpVector(p, end, .5), strength, (wa+wb)*.5, guides) {
			break
		}
		// Tree limbs stop before joining older limbs. Ignore the immediate
		// attachment neighborhood so a child can leave its parent's shoulder.
		blocked := false
		for _, other := range branches[:start] {
			if other.A.Sub(root).Len() < .09 || other.B.Sub(root).Len() < .09 {
				continue
			}
			if geom.SegmentDistanceSquared(p, end, other.A, other.B) < math.Pow((wb+other.WidthB)*1.15, 2) {
				blocked = true
				break
			}
		}
		if blocked {
			break
		}
		branches = append(branches, BranchSegment{A: p, B: end, WidthA: wa, WidthB: wb, LightA: strength, LightB: strength})
		p, d = end, next
	}
	end := len(branches)
	if depth >= 2 || end-start < 8 {
		return branches
	}
	for _, fraction := range []float64{.35, .65} {
		parent := branches[start+int(float64(end-start-1)*fraction)]
		side := 1.0
		if rng.Intn(2) == 0 {
			side = -1
		}
		dir := geom.Rotate(parent.B.Sub(parent.A).Norm(), side*geom.Lerp(.65, 1.05, rng.Float64()))
		branches = growRecursiveBranch(branches, parent.B, dir, reach*geom.Lerp(.5, .7, rng.Float64()), math.Max(shape.branchWidth(0), parent.WidthB*.8), depth+1, noise, guides, rng, shape)
	}
	return branches
}
