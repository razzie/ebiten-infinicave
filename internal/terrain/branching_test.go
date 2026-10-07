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
