package terrain

import (
	"math"
	"math/rand"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestBackgroundSeedsWorkWithoutGuides(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	noise := NewPerlin(rng)
	seeds := generateSeeds(rng, 80, noise)
	relaxSeeds(seeds, noise)
	for _, p := range seeds {
		spacing := desiredSpacing(p, noise)
		if math.IsNaN(p.X) || math.IsNaN(p.Y) || p.X < 0 || p.X > generationWidth || p.Y < GenerationMinY || p.Y > generationMaxY {
			t.Fatalf("invalid background seed: %v", p)
		}
		if math.IsNaN(spacing) || spacing < .022 || spacing > .030 {
			t.Fatalf("invalid background spacing at %v: %v", p, spacing)
		}
	}
}

func TestWorldSeedsMatchOverlappingWindows(t *testing.T) {
	seed := int64(42)
	aTop, bTop := SectionTop(0), SectionTop(1)
	noise := NewPerlin(rand.New(rand.NewSource(seed)))
	noise.OffsetY = aTop
	a := worldSeeds(seed, aTop, noise)
	noise.OffsetY = bTop
	b := worldSeeds(seed, bTop, noise)
	collect := func(seeds []geom.V, top float64) []geom.V {
		var result []geom.V
		for _, p := range seeds {
			p.Y += top
			if p.Y > -1.900 && p.Y < -.100 {
				result = append(result, p)
			}
		}
		return result
	}
	left, right := collect(a, aTop), collect(b, bTop)
	if len(left) != len(right) {
		t.Fatal("overlapping sections use different terrain site counts")
	}
	for i, p := range left {
		if p.Sub(right[i]).Len() > 1e-9 {
			t.Fatal("overlapping sections use different terrain sites")
		}
	}
}
