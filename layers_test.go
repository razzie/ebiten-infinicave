package main

import (
	"image/color"
	"math"
	"math/rand"
	"testing"
)

func TestBackgroundContainsBlackAndDarkCells(t *testing.T) {
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	tones := make(map[color.NRGBA]bool)
	for y := 0.0; y < H; y += 20 {
		for x := 0.0; x < W; x += 20 {
			clr := backgroundCellColor(V{x, y}, noise)
			if clr.A != 255 || clr.R > 50 || clr.G > 51 || clr.B > 49 {
				t.Fatalf("background must stay opaque and dark: %v", clr)
			}
			tones[clr] = true
		}
	}
	if !tones[color.NRGBA{A: 255}] || len(tones) < 10 {
		t.Fatalf("expected exact black and varied charcoal cells, got %d tones", len(tones))
	}
}

func TestGuideLayerOccupancyIsOpaqueAndIndependentOfLight(t *testing.T) {
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	guides := []Guide{splineGuide([]V{{100, 400}, {900, 400}}, 1)}
	for _, p := range []V{{500, 100}, {500, 390}, {500, 900}} {
		if clr := guideCellColor(p, guides, noise, nil); clr != (color.NRGBA{}) {
			t.Errorf("unlit guide cell at %v must be transparent, got %v", p, clr)
		}
	}
	if clr := guideCellColor(V{500, 408}, nil, noise, nil); clr.A != 0 {
		t.Errorf("empty guide grid must be transparent, got %v", clr)
	}
	crest := guideCellColor(V{500, 408}, guides, noise, nil)
	if crest.A != 255 || crest.R < 75 || crest.R > 175 {
		t.Errorf("crest must remain bright and opaque, got %v", crest)
	}
	solid, empty := 0, 0
	for y := 420.0; y < 680; y += 5 {
		clr := guideCellColor(V{500, y}, guides, noise, nil)
		switch clr.A {
		case 0:
			empty++
		case 255:
			solid++
		default:
			t.Fatal("solid rock must not fade through the background")
		}
	}
	if solid == 0 || empty == 0 {
		t.Fatal("relief must have an opaque shoulder and a finite footprint")
	}

	// An isolated offshoot remains visible in the subdued flank palette.
	p := V{500, 650}
	branches := []BranchSegment{{
		A: V{500, 620}, B: V{500, 680},
		WidthA: 30, WidthB: 20, LightA: .4, LightB: .3,
	}}
	if clr := guideCellColor(p, guides, noise, branches); clr.A != 255 || clr.R < 25 {
		t.Errorf("branch must reveal foreground faces beyond the guide band, got %v", clr)
	}
}

func TestBackgroundSeedsWorkWithoutGuides(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	noise := NewPerlin(rng)
	seeds := generateSeeds(rng, 80, noise)
	relaxSeeds(seeds, noise)
	for _, p := range seeds {
		spacing := desiredSpacing(p, noise)
		if math.IsNaN(p.X) || math.IsNaN(p.Y) || p.X < 0 || p.X > W || p.Y < 0 || p.Y > H {
			t.Fatalf("invalid background seed: %v", p)
		}
		if math.IsNaN(spacing) || spacing < 22 || spacing > 30 {
			t.Fatalf("invalid background spacing at %v: %v", p, spacing)
		}
	}
}
