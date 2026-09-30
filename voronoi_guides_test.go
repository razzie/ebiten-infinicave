package main

import (
	"math"
	"math/rand"
	"testing"
)

func TestGuideRocksAreIrregularAndCrestIsBalanced(t *testing.T) {
	g := splineGuide([]V{{100, 500}, {900, 500}}, 1)
	seeds := addGuideSeeds([]V{{500, 505}, {500, 800}}, []Guide{g}, rand.New(rand.NewSource(42)))
	rows := [3]int{}
	crestCount, brightCount, darkCount := 0, 0, 0
	for _, p := range seeds {
		if p.Y > 500 && p.Y < 588 {
			brightCount++
			if p.Y < 518 {
				crestCount++
			}
		} else if p.Y < 500 {
			darkCount++
		}
	}
	// Broad crest rocks should average at least 32 pixels along a straight
	// guide, with fewer bright cells overall than on the shadow side.
	if crestCount > 26 || brightCount > 80 || brightCount >= darkCount {
		t.Errorf("guide cells are too dense: crest=%d bright=%d dark=%d", crestCount, brightCount, darkCount)
	}
	widthSum, heightSum := 0.0, 0.0
	minWidth, maxWidth := math.Inf(1), 0.0
	irregular, total := 0, 0
	for i, p := range seeds {
		if p.X < 250 || p.X > 750 || p.Y <= 500 || p.Y > 565 {
			continue
		}
		poly := voronoiCell(i, seeds)
		minX, minY := math.Inf(1), math.Inf(1)
		maxX, maxY := math.Inf(-1), math.Inf(-1)
		for _, v := range poly {
			minX, minY = math.Min(minX, v.X), math.Min(minY, v.Y)
			maxX, maxY = math.Max(maxX, v.X), math.Max(maxY, v.Y)
		}
		width, height := maxX-minX, maxY-minY
		widthSum += width
		heightSum += height
		minWidth, maxWidth = math.Min(minWidth, width), math.Max(maxWidth, width)
		total++
		if len(poly) >= 5 {
			irregular++
		}
		switch {
		case p.Y < 518:
			rows[0]++
		case p.Y < 539:
			rows[1]++
		default:
			rows[2]++
		}
	}
	// Individual rocks may be squat or pointed; the band should remain
	// elongated overall without reverting to repeated rectangular tiles.
	if widthSum < 1.2*heightSum {
		t.Errorf("guide rocks lost their overall flattening: width/height %.2f", widthSum/heightSum)
	}
	if irregular*2 < total || maxWidth < 1.4*minWidth {
		t.Errorf("guide rocks are too uniform: %d/%d irregular polygons, width range %.2f–%.2f", irregular, total, minWidth, maxWidth)
	}
	for row, count := range rows {
		if count == 0 {
			t.Errorf("missing bright row %d", row)
		}
	}
	for x := 150.0; x <= 850; x++ {
		p := V{x, 500}
		dark, light := math.Inf(1), math.Inf(1)
		for _, s := range seeds {
			if s.Y < 500 {
				dark = math.Min(dark, p.Sub(s).Len2())
			} else {
				light = math.Min(light, p.Sub(s).Len2())
			}
		}
		if math.Abs(dark-light) > 1e-7 {
			t.Fatalf("crest deviates from guide at x=%v: squared distances %v, %v", x, dark, light)
		}
	}
}

func TestCurvedGuidesUseCloserSamplesAndContinuousNormals(t *testing.T) {
	g := splineGuide([]V{{100, 400}, {240, 400}, {280, 440}, {240, 490}, {150, 500}}, 1)
	length := g.S[len(g.S)-1]
	samples := guideSamples(&g, guideSideSpacing(1), rand.New(rand.NewSource(42)))
	if len(samples) <= int(math.Ceil(length/guideSideSpacing(1)))+1 {
		t.Fatal("curved guide did not get additional samples")
	}
	for i := 1; i < len(samples); i++ {
		a, _, _ := g.frameAt(samples[i-1])
		b, _, _ := g.frameAt(samples[i])
		mid, _, _ := g.frameAt((samples[i-1] + samples[i]) * .5)
		if err := mid.Sub(lerpV(a, b, .5)).Len(); err > 1.4 {
			t.Errorf("chord error %.3f exceeds smoothing tolerance", err)
		}
	}
	for _, s := range g.S[1 : len(g.S)-1] {
		_, _, before := g.frameAt(s - 1e-5)
		_, _, after := g.frameAt(s + 1e-5)
		if before.Dot(after) < .99999 {
			t.Errorf("normal jumps at arc length %v", s)
		}
	}
}
