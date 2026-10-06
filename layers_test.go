package infinicave

import (
	"image/color"
	"math"
	"math/rand"
	"testing"
)

func TestBackgroundContainsBlackAndDarkCells(t *testing.T) {
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	tones := make(map[color.NRGBA]bool)
	for y := float64(generationMinY); y < generationMaxY; y += 0.02 {
		for x := 0.0; x < generationWidth; x += 0.02 {
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
	for y := float64(generationMinY); y < generationMaxY; y += 0.02 {
		for x := 0.0; x < generationWidth; x += 0.02 {
			p := V{x, y}
			lit := backgroundSurfaceColor(p, noise, V3{0, -1, 0})
			shadowed := backgroundSurfaceColor(p, noise, V3{0, 1, 0})
			if lit.R > shadowed.R {
				return
			}
		}
	}
	t.Fatal("background directional lighting has no visible contrast")
}

func TestGuideLayerOccupancyIsOpaqueAndIndependentOfLight(t *testing.T) {
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	guides := []Guide{splineGuide([]V{{0.1, 0.4}, {0.9, 0.4}}, 1)}
	for _, p := range []V{{0.5, 0.1}, {0.5, 0.39}, {0.5, 0.9}} {
		if clr := guideCellColor(p, guides, noise, nil); clr != (color.NRGBA{}) {
			t.Errorf("unlit guide cell at %v must be transparent, got %v", p, clr)
		}
	}
	if clr := guideCellColor(V{0.5, 0.408}, nil, noise, nil); clr.A != 0 {
		t.Errorf("empty guide grid must be transparent, got %v", clr)
	}
	crest := guideCellColor(V{0.5, 0.408}, guides, noise, nil)
	if crest.A != 255 || crest.R < 75 || crest.R > 175 {
		t.Errorf("crest must remain bright and opaque, got %v", crest)
	}
	solid, empty := 0, 0
	for y := 0.42; y < 0.68; y += 0.005 {
		clr := guideCellColor(V{0.5, y}, guides, noise, nil)
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
	p := V{0.5, 0.65}
	branches := newBranchField([]BranchSegment{{
		A: V{0.5, 0.62}, B: V{0.5, 0.68},
		WidthA: .030, WidthB: .020, LightA: .4, LightB: .3,
	}})
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
		if math.IsNaN(p.X) || math.IsNaN(p.Y) || p.X < 0 || p.X > generationWidth || p.Y < generationMinY || p.Y > generationMaxY {
			t.Fatalf("invalid background seed: %v", p)
		}
		if math.IsNaN(spacing) || spacing < .022 || spacing > .030 {
			t.Fatalf("invalid background spacing at %v: %v", p, spacing)
		}
	}
}
