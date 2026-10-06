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
		{"crossing interiors", V{0, 0}, V{100, 0}, V{50, -50}, V{50, 50}, 0},
		{"parallel", V{0, 0}, V{100, 0}, V{20, 30}, V{80, 30}, 900},
		{"collinear overlap", V{0, 0}, V{100, 0}, V{50, 0}, V{150, 0}, 0},
		{"endpoints", V{0, 0}, V{100, 0}, V{130, 40}, V{130, 100}, 2500},
		{"point segment", V{50, 30}, V{50, 30}, V{0, 0}, V{100, 0}, 900},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := guideSegmentsDistance2(tc.a, tc.b, tc.c, tc.d); math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("distance squared = %v, want %v", got, tc.want)
			}
		})
	}
	if guideSelfClear(splineGuide([]V{{100, 100}, {600, 100}, {600, 200}, {100, 200}}, 1)) {
		t.Fatal("tight returning curve was accepted")
	}
	if !guideSelfClear(splineGuide([]V{{100, 100}, {500, 80}, {700, 300}}, 1)) {
		t.Fatal("open curve was rejected")
	}
}

func guideSampleCoverage(guides []Guide, minY, maxY float64) float64 {
	covered, total := 0, 0
	for y := minY + 25; y < maxY; y += 50 {
		for x := 25.0; x < W; x += 50 {
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
	lo, hi := V{W, W}, V{}
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
		coverage := guideSampleCoverage(guides, 0, W)
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
			if g.Min.X < foregroundScreenInset || g.Max.X > W-foregroundScreenInset || g.Min.Y < 0 || g.Max.Y > W {
				t.Fatal("guide escaped its placement bounds")
			}
		}
	}
	if hi.X-lo.X < W*.35 || hi.Y-lo.Y < W*.35 || maxLength-minLength < 250 || len(counts) < 3 {
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
			coverage := guideSampleCoverage(guides, W, 2*W)
			if coverage < .55 || coverage > .85 {
				t.Errorf("seed %d: streamed section coverage %.3f", seed, coverage)
			}
		}
		bySeed := make(map[int64]Guide)
		for _, g := range a {
			bySeed[g.Seed] = shiftedGuide(g, sectionWindowTop(id))
		}
		shared := 0
		for _, g := range b {
			other, ok := bySeed[g.Seed]
			if !ok {
				continue
			}
			shared++
			g = shiftedGuide(g, sectionWindowTop(id+1))
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
