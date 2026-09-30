package main

import (
	"math"
	"math/rand"
	"reflect"
	"testing"
)

func TestBranchesExtendBeyondGuideBandAndLeaveDarkGaps(t *testing.T) {
	guides := []Guide{splineGuide([]V{{100, 400}, {900, 400}}, 1)}
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	branches := generateBranches(guides, noise, rand.New(rand.NewSource(42)))
	lit, dark := 0, 0
	for y := 400 + guideInfluence; y <= 400+guideInfluence+40; y += 10 {
		for x := 100.0; x <= 900; x += 10 {
			p := V{x, y}
			base := guideBias(p, guides, noise, nil)
			got := guideBias(p, guides, noise, branches)
			if got > base+.06 {
				lit++
			}
			if got < .01 {
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
		splineGuide([]V{{100, 400}, {900, 400}}, 1),
		// A second ridge intercepts some of the first ridge's branches.
		splineGuide([]V{{100, 570}, {900, 570}}, 1),
	}
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	branches := generateBranches(guides, noise, rand.New(rand.NewSource(42)))
	for _, y := range []float64{392, 375, 350, 562, 545, 520} {
		for x := 120.0; x <= 880; x += 10 {
			p := V{x, y}
			want := guideBias(p, guides, noise, nil)
			got := guideBias(p, guides, noise, branches)
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
