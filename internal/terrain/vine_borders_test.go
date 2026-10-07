package terrain

import (
	"image/color"
	"math"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestVineBordersUseBackgroundGeometry(t *testing.T) {
	dark := color.NRGBA{30, 30, 30, 255}
	background := testRockGrid([]geom.V{{X: 0.25, Y: 0.5}, {X: 0.75, Y: 0.5}}, []color.NRGBA{dark, dark})
	foreground := testRockGrid([]geom.V{{X: 0.5, Y: 0.25}, {X: 0.5, Y: 0.75}}, []color.NRGBA{dark, dark})
	field := newVineTerrain(background, foreground)
	for _, p := range []geom.V{{X: 0.5, Y: 0.25}, {X: 0.5, Y: 0.75}} {
		if got := field.borderDistance(p); got > vineFieldStep {
			t.Fatalf("equal-tone background seam missing at %v: %.2f", p, got)
		}
	}
	for _, p := range []geom.V{{X: 0.25, Y: 0.5}, {X: 0.001, Y: 0.5}, {X: 0.25, Y: GenerationMinY + .001}, {X: generationWidth - 0.001, Y: 0.5}, {X: 0.25, Y: generationMaxY - .001}} {
		if got := field.borderDistance(p); got != vineBorderRange {
			t.Fatalf("foreground seam or window edge attracts vines at %v: %.2f", p, got)
		}
	}
}

func TestVineFollowsSeamsAndRoundsJunction(t *testing.T) {
	dark := color.NRGBA{30, 30, 30, 255}
	// Three equal-tone cells meet at (0.5, 0.4875). A downward-growing vine
	// must turn onto a diagonal seam, although brightness offers no guidance.
	background := testRockGrid([]geom.V{{X: 0.45, Y: 0.45}, {X: 0.55, Y: 0.45}, {X: 0.5, Y: 0.55}}, []color.NRGBA{dark, dark, dark})
	field := newVineTerrain(background, nil)
	vine := growVine(field, geom.V{X: 0.493, Y: 0.3}, geom.V{X: 0, Y: 1}, 0.005, 0.4, 0, 1, 0)
	length, span := vine.extent()
	if length < .350 || span < .250 {
		t.Fatalf("vine stopped at the cell junction: length %.1f, span %.1f", length, span)
	}
	close, samples, totalTurn := 0, 0, 0.0
	for i, p := range vine.Points {
		if i < 20 {
			continue // Allow the root to converge onto the seam.
		}
		// Measure against polygon segments directly, independently of the field.
		distance := math.Inf(1)
		for _, cell := range background {
			for j, a := range cell.Polygon {
				b := cell.Polygon[(j+1)%len(cell.Polygon)]
				d := b.Sub(a)
				if d.Len2() > 0 {
					q := a.Add(d.Mul(geom.Clamp(p.P.Sub(a).Dot(d)/d.Len2(), 0, 1)))
					distance = math.Min(distance, p.P.Sub(q).Len())
				}
			}
		}
		samples++
		if distance < .006 {
			close++
		}
		a := vine.Points[i-1].P.Sub(vine.Points[i-2].P).Norm()
		b := p.P.Sub(vine.Points[i-1].P).Norm()
		turn := math.Atan2(a.X*b.Y-a.Y*b.X, a.Dot(b))
		if math.Abs(turn) > .26 {
			t.Fatalf("sharp corner instead of a rounded junction: %.3f radians", turn)
		}
		totalTurn += turn
	}
	if float64(close)/float64(samples) < .9 {
		t.Fatalf("vine cuts across cell interiors: only %d/%d points near seams", close, samples)
	}
	if math.Abs(totalTurn) < .7 {
		t.Fatal("vine did not turn onto a diagonal seam")
	}
}
