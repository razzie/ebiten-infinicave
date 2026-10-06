package infinicave

import (
	"math"
	"math/rand"
	"reflect"
	"testing"
)

func TestGuideSegmentClearance(t *testing.T) {
	for _, tc := range []struct {
		name       string
		a, b, c, d V
		want       float64
	}{
		{"crossing interiors", V{0, 0}, V{0.1, 0}, V{0.05, -0.05}, V{0.05, 0.05}, 0},
		{"parallel", V{0, 0}, V{0.1, 0}, V{0.02, 0.03}, V{0.08, 0.03}, .0009},
		{"collinear overlap", V{0, 0}, V{0.1, 0}, V{0.05, 0}, V{0.15, 0}, 0},
		{"endpoints", V{0, 0}, V{0.1, 0}, V{0.13, 0.04}, V{0.13, 0.1}, .0025},
		{"point segment", V{0.05, 0.03}, V{0.05, 0.03}, V{0, 0}, V{0.1, 0}, .0009},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := guideSegmentsDistance2(tc.a, tc.b, tc.c, tc.d); math.Abs(got-tc.want) > 1e-15 {
				t.Fatalf("distance squared = %v, want %v", got, tc.want)
			}
		})
	}
	if guideSelfClear(splineGuide([]V{{0.1, 0.1}, {0.6, 0.1}, {0.6, 0.2}, {0.1, 0.2}}, 1)) {
		t.Fatal("tight returning curve was accepted")
	}
	if !guideSelfClear(splineGuide([]V{{0.1, 0.1}, {0.5, 0.08}, {0.7, 0.3}}, 1)) {
		t.Fatal("open curve was rejected")
	}
}

func guideSampleCoverage(guides []Guide, minY, maxY float64) float64 {
	covered, total := 0, 0
	for y := minY + 0.025; y < maxY; y += 0.05 {
		for x := 0.025; x < generationWidth; x += 0.05 {
			_, pr := nearestGuide(V{x, y}, guides)
			if pr.Dist <= guideCoverageRadius {
				covered++
			}
			total++
		}
	}
	return float64(covered) / float64(total)
}

func checkGuideClearance(t *testing.T, guides []Guide) {
	t.Helper()
	for i, g := range guides {
		if !guideSelfClear(g) {
			t.Fatalf("guide %d folds too close to itself", i)
		}
		for j := i + 1; j < len(guides); j++ {
			if guidesTooClose(g, guides[j]) {
				t.Fatalf("guides %d and %d overlap or lack clearance", i, j)
			}
		}
	}
}

func TestRandomGuideSectionsHaveCoverageClearanceAndVariety(t *testing.T) {
	lo, hi := V{generationWidth, generationWidth}, V{}
	minLength, maxLength := math.Inf(1), 0.0
	counts := map[int]bool{}
	for seed := int64(0); seed < 32; seed++ {
		guides := generateGuideSection(rand.New(rand.NewSource(seed)))
		layout := newGuideLayout(rand.New(rand.NewSource(seed)))
		if len(guides) == 0 {
			t.Fatalf("seed %d generated no guides", seed)
		}
		checkGuideClearance(t, guides)
		for _, pocket := range layout.pockets {
			_, pr := nearestGuide(pocket.center, guides)
			if pr.Dist < pocket.radius {
				t.Fatalf("seed %d filled a reserved open pocket", seed)
			}
		}
		coverage := guideSampleCoverage(guides, 0, generationWidth)
		t.Logf("seed %d: %d guides, coverage %.3f", seed, len(guides), coverage)
		if coverage < .55 || coverage > .85 {
			t.Errorf("seed %d: coverage %.3f leaves too much empty space or too few gaps", seed, coverage)
		}
		counts[len(guides)] = true
		center := lerpV(guides[0].Min, guides[0].Max, .5)
		lo = V{math.Min(lo.X, center.X), math.Min(lo.Y, center.Y)}
		hi = V{math.Max(hi.X, center.X), math.Max(hi.Y, center.Y)}
		for _, g := range guides {
			length := g.S[len(g.S)-1]
			minLength, maxLength = math.Min(minLength, length), math.Max(maxLength, length)
			if g.Min.X < foregroundScreenInset || g.Max.X > generationWidth-foregroundScreenInset || g.Min.Y < 0 || g.Max.Y > generationWidth {
				t.Fatal("guide escaped its placement bounds")
			}
		}
	}
	if hi.X-lo.X < generationWidth*.35 || hi.Y-lo.Y < generationWidth*.35 || maxLength-minLength < .250 || len(counts) < 3 {
		t.Fatalf("scenes still repeat anchors, lengths, or counts: centers %v..%v, lengths %.1f..%.1f, counts %v", lo, hi, minLength, maxLength, counts)
	}
	a := generateGuideSection(rand.New(rand.NewSource(42)))
	if !reflect.DeepEqual(a, generateGuideSection(rand.New(rand.NewSource(42)))) {
		t.Fatal("same seed did not reproduce its curves")
	}
}

func TestWorldGuidesKeepClearanceCoverageAndStableOverlaps(t *testing.T) {
	for _, tc := range []struct{ seed, id int64 }{{1, 0}, {42, 0}, {100, 0}, {7, 37}, {9, -7}} {
		seed, id := tc.seed, tc.id
		a, b := worldGuides(seed, id), worldGuides(seed, id+1)
		checkGuideClearance(t, a)
		checkGuideClearance(t, b)
		for _, guides := range [][]Guide{a, b} {
			coverage := guideSampleCoverage(guides, 0, SectionHeight)
			if coverage < .55 || coverage > .85 {
				t.Errorf("seed %d: streamed section coverage %.3f", seed, coverage)
			}
		}
		bySeed := make(map[int64]Guide)
		for _, g := range a {
			bySeed[g.Seed] = shiftedGuide(g, sectionTop(id))
		}
		shared := 0
		for _, g := range b {
			other, ok := bySeed[g.Seed]
			if !ok {
				continue
			}
			shared++
			g = shiftedGuide(g, sectionTop(id+1))
			if len(g.Pts) != len(other.Pts) || !reflect.DeepEqual(g.S, other.S) {
				t.Fatal("overlapping windows changed a guide's shape")
			}
			for i, p := range g.Pts {
				if p.Sub(other.Pts[i]).Len() > 1e-9 {
					t.Fatal("overlapping windows moved a guide")
				}
			}
		}
		if shared == 0 {
			t.Fatal("neighboring windows shared no guides")
		}
	}
}
