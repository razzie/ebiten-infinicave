package terrain

import (
	"math"
	"math/rand"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestBranchesExtendReliefBeyondGuideBandAndLeaveGaps(t *testing.T) {
	guides := []Guide{SplineGuide([]geom.V{{X: 0.1, Y: 0.4}, {X: 0.9, Y: 0.4}}, 1)}
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	branches := newBranchField(generateBranches(guides, noise, rand.New(rand.NewSource(42))))
	lit, dark := 0, 0
	for y := 0.4 + guideInfluence; y <= 0.4+guideInfluence+0.04; y += 0.01 {
		for x := 0.1; x <= 0.9; x += 0.01 {
			p := geom.V{X: x, Y: y}
			base := reliefHeight(p, guides, noise, nil)
			got := reliefHeight(p, guides, noise, branches)
			if got > base+.006 {
				lit++
			}
			if got < .00001 {
				dark++
			}
		}
	}
	if lit < 10 || dark < 10 {
		t.Errorf("expected distant fingers separated by dark gaps: lit=%d dark=%d", lit, dark)
	}
}

func TestBranchesPreserveGuideShadows(t *testing.T) {
	guides := []Guide{
		SplineGuide([]geom.V{{X: 0.1, Y: 0.4}, {X: 0.9, Y: 0.4}}, 1),
		// A second ridge intercepts some of the first ridge's branches.
		SplineGuide([]geom.V{{X: 0.1, Y: 0.57}, {X: 0.9, Y: 0.57}}, 1),
	}
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	branches := newBranchField(generateBranches(guides, noise, rand.New(rand.NewSource(42))))
	for _, y := range []float64{.392, .375, .350, .562, .545, .520} {
		for x := 0.12; x <= 0.88; x += 0.01 {
			p := geom.V{X: x, Y: y}
			want := reliefHeight(p, guides, noise, nil)
			got := reliefHeight(p, guides, noise, branches)
			if math.Abs(got-want) > 1e-12 {
				t.Fatalf("branch spills onto shadow at %v: got %v, want %v", p, got, want)
			}
		}
	}
}

func TestBranchRegenerationIsDeterministic(t *testing.T) {
	guides := generateGuides(rand.New(rand.NewSource(42)))
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	a := generateBranches(guides, noise, rand.New(rand.NewSource(42)))
	b := generateBranches(guides, noise, rand.New(rand.NewSource(42)))
	c := generateBranches(guides, noise, rand.New(rand.NewSource(43)))
	if len(a) == 0 || !reflect.DeepEqual(a, b) || reflect.DeepEqual(a, c) {
		t.Fatal("branches must reproduce the same seed and vary with a new seed")
	}
}

func TestBranchesStartInSolidRidgesAndKeepConnectedCenterlines(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		for _, seed := range []int64{1, 42, 93} {
			guides := newGuideCache(seed, orientation).window(0)
			noise := NewPerlin(rand.New(rand.NewSource(seed)))
			noise.OffsetY = SectionTop(0)
			segments := generateBranches(guides, noise, rand.New(rand.NewSource(seed)))
			field := newBranchField(segments)
			ends := make(map[geom.V]bool)
			roots := 0
			for _, b := range segments {
				if !ends[b.A] {
					roots++
					if reliefHeight(b.A, guides, noise, nil) <= rockContourHeight {
						t.Fatalf("%v seed %d: spur starts outside solid rock at %v", orientation, seed, b.A)
					}
				}
				for _, u := range []float64{0, .5, 1} {
					p := geom.LerpVector(b.A, b.B, u)
					if reliefHeight(p, guides, noise, field) <= rockContourHeight {
						t.Fatalf("%v seed %d: spur has an invisible centerline at %v", orientation, seed, p)
					}
				}
				ends[b.B] = true
			}
			if roots == 0 {
				t.Fatalf("%v seed %d: all spurs were removed", orientation, seed)
			}
		}
	}
}

func TestBranchesDoNotReappearPastGuideShadow(t *testing.T) {
	// A spur from a full-sized guide used to dip below the contour through
	// another guide's shadow, then reappear past its tip as a detached chip.
	guides := worldGuides(93, 0)
	noise := NewPerlin(rand.New(rand.NewSource(93)))
	noise.OffsetY = SectionTop(0)
	field := newBranchField(generateBranches(guides, noise, rand.New(rand.NewSource(93))))
	for _, p := range []geom.V{{X: .2775872186, Y: .6832382693}, {X: .2776039460, Y: .6833272521}} {
		if height := reliefHeight(p, guides, noise, field); height >= rockContourHeight {
			t.Fatalf("detached branch tip reappeared at %v: height %v", p, height)
		}
	}
}

func TestBranchForkStopsBeforeShadowAndScreenFade(t *testing.T) {
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	start := geom.V{X: .5, Y: .3}
	direction := geom.V{Y: 1}
	guide := SplineGuide([]geom.V{{X: .1, Y: .5}, {X: .9, Y: .5}}, 1)
	open := appendBranchFork(nil, start, direction, .4, .03, .5, 0, noise, nil)
	blocked := appendBranchFork(nil, start, direction, .4, .03, .5, 0, noise, []Guide{guide})
	if len(blocked) == 0 || len(blocked) >= len(open) {
		t.Fatal("fork did not stop when approaching the guide's shadow")
	}
	for _, b := range blocked {
		if b.B.Y >= .4 {
			t.Fatalf("fork continued through the shadow toward the lit side: %v", b.B)
		}
	}
	// The edge fade can erase a fork root even when the curve later bends
	// inward to visible terrain. It must never start in that erased region.
	if fork := appendBranchFork(nil, geom.V{X: .025, Y: .3}, geom.V{X: 1}, .4, .03, .5, 0, noise, nil); len(fork) != 0 {
		t.Fatal("fork grew from an invisible root at the screen edge")
	}
}

func TestBranchForkDoesNotGrowBelowTerrainSamplingWidth(t *testing.T) {
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	start, direction := geom.V{X: .5, Y: .3}, geom.V{Y: 1}
	if fork := appendBranchFork(nil, start, direction, .4, .004, .5, 0, noise, nil); len(fork) != 0 {
		t.Fatal("a spur too narrow for terrain samples was generated")
	}
	if fork := appendBranchFork(nil, start, direction, .4, .03, .5, 0, noise, nil); len(fork) == 0 {
		t.Fatal("a spur with a resolvable solid width was removed")
	}
}
