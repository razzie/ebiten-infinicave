package terrain

import (
	"math"
	"math/rand"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// Site density follows terrain only; guides cut faces without adding sites.
func desiredSpacing(p geom.V, noise *Perlin) float64 {
	return geom.Lerp(.030, .022, geom.Smoothstep(.39, .58, fbm(noise, p)))
}

func generateSeeds(rng *rand.Rand, count int, noise *Perlin) []geom.V {
	// Weighted sampling follows background terrain.
	pts := make([]geom.V, 0, count)
	for len(pts) < count {
		best := geom.V{}
		bestD2 := -1.0
		candidates := 22
		if len(pts) < 8 {
			candidates = 8
		}
		for c := 0; c < candidates; c++ {
			p := geom.V{X: rng.Float64() * generationWidth, Y: GenerationMinY + rng.Float64()*GenerationHeight}
			minD2 := math.Inf(1)
			for _, q := range pts {
				d2 := p.Sub(q).Len2()
				if d2 < minD2 {
					minD2 = d2
				}
			}
			spacing := desiredSpacing(p, noise)
			minD2 /= spacing * spacing
			if minD2 > bestD2 {
				best, bestD2 = p, minD2
			}
		}
		pts = append(pts, best)
	}
	return pts
}

// Repel crowded seeds without forcing a regular lattice.
func relaxSeeds(seeds []geom.V, noise *Perlin) {
	spacing := make([]float64, len(seeds))
	for i, p := range seeds {
		spacing[i] = desiredSpacing(p, noise) * .48
	}
	for pass := 0; pass < 4; pass++ {
		shifts := make([]geom.V, len(seeds))
		for i, p := range seeds {
			for j := i + 1; j < len(seeds); j++ {
				d := p.Sub(seeds[j])
				distance := d.Len()
				minimum := (spacing[i] + spacing[j]) * .5
				if distance >= minimum {
					continue
				}
				push := d.Norm().Mul((minimum - distance) * .35)
				shifts[i] = shifts[i].Add(push)
				shifts[j] = shifts[j].Sub(push)
			}
		}
		for i := range seeds {
			seeds[i] = seeds[i].Add(shifts[i])
			seeds[i].X = geom.Clamp(seeds[i].X, .001, generationWidth-.001)
			seeds[i].Y = geom.Clamp(seeds[i].Y, GenerationMinY+.001, generationMaxY-.001)
		}
	}
}

func SectionSeed(seed, id int64) int64 {
	x := uint64(seed) + uint64(id)*0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return int64(x ^ (x >> 31))
}

func cellSeed(seed int64, p geom.V, top float64) int64 {
	return SectionSeed(SectionSeed(seed, int64(math.Round(p.X*1e6))), int64(math.Round((p.Y+top)*1e6)))
}

// Jittered world-space sites give neighboring generation windows exactly the
// same rocks in their overlap, independent of load order or cache eviction.
func worldSeeds(seed int64, top float64, noise *Perlin) []geom.V {
	return worldSeedsInRange(seed, top, noise, 0, generationWidth)
}

func worldSeedsInRange(seed int64, top float64, noise *Perlin, minX, maxX float64) []geom.V {
	const step = .022
	first := int64(math.Floor((top + GenerationMinY) / step))
	last := int64(math.Ceil((top + generationMaxY) / step))
	rows := make([][]geom.V, last-first)
	parallelFor(len(rows), func(n int) {
		row := first + int64(n)
		for col := int64(math.Floor(minX / step)); float64(col)*step < maxX; col++ {
			p := geom.V{X: (float64(col)+.5)*step + (SiteRandom(seed, row, col, 0)-.5)*step*.85,
				Y: (float64(row)+.5)*step + (SiteRandom(seed, row, col, 1)-.5)*step*.85 - top}
			if p.X <= minX+.001 || p.X >= maxX-.001 || p.Y <= GenerationMinY+.001 || p.Y >= generationMaxY-.001 {
				continue
			}
			spacing := desiredSpacing(p, noise)
			if SiteRandom(seed, row, col, 2) < min(1, step*step/(spacing*spacing)) {
				rows[n] = append(rows[n], p)
			}
		}
	})
	var seeds []geom.V
	for _, r := range rows {
		seeds = append(seeds, r...)
	}
	return seeds
}

// Three independent samples per world site need no stateful RNG or seed table.
// Coordinates and channel, rather than section/window order, choose the sample.
func SiteRandom(seed, row, col, channel int64) float64 {
	x := SectionSeed(SectionSeed(SectionSeed(seed, row), col), channel)
	return float64(uint64(x)>>11) * (1.0 / (1 << 53))
}
