package infinicave

import (
	"fmt"
	"math"
)

// HoleShape selects a circular blast or a flat-ended rectangular drill cut.
type HoleShape uint8

const (
	HoleCircle HoleShape = iota
	HoleSegment
)

// Hole describes a foreground rock cut in scene units. Circle uses Center and
// Radius; Segment uses Start, End and full Width. Only the selected fields are
// read. Carve uses world coordinates; SectionContent uses section-local points.
// Radii and widths must be positive and finite; segment endpoints distinct.
type Hole struct {
	Shape      HoleShape
	Center     V
	Radius     float64
	Start, End V
	Width      float64
}

func (h Hole) translatedY(dy float64) Hole {
	if h.Shape == HoleCircle {
		h.Center.Y += dy
	} else {
		h.Start.Y += dy
		h.End.Y += dy
	}
	return h
}

func (h Hole) rockCut() (rockCut, error) {
	var poly []V
	switch h.Shape {
	case HoleCircle:
		if !finiteCarvePoint(h.Center) || !finiteCarveValue(h.Radius) || h.Radius <= 0 {
			return rockCut{}, fmt.Errorf("infinicave: blast requires a finite center and positive finite radius")
		}
		const sides = 96
		poly = make([]V, sides)
		for i := range poly {
			angle := 2 * math.Pi * float64(i) / sides
			poly[i] = h.Center.Add(V{math.Cos(angle) * h.Radius, math.Sin(angle) * h.Radius})
		}
	case HoleSegment:
		length := math.Hypot(h.End.X-h.Start.X, h.End.Y-h.Start.Y)
		if !finiteCarvePoint(h.Start) || !finiteCarvePoint(h.End) || !finiteCarveValue(h.Width) ||
			h.Width <= 0 || !finiteCarveValue(length) || length == 0 {
			return rockCut{}, fmt.Errorf("infinicave: segment cut requires distinct finite endpoints and positive finite width")
		}
		normal := V{(h.End.X - h.Start.X) / length, (h.End.Y - h.Start.Y) / length}.Perp().Mul(h.Width / 2)
		poly = []V{h.Start.Sub(normal), h.End.Sub(normal), h.End.Add(normal), h.Start.Add(normal)}
	default:
		return rockCut{}, fmt.Errorf("infinicave: invalid hole shape %v", h.Shape)
	}
	for _, p := range poly {
		if !finiteCarvePoint(p) {
			return rockCut{}, fmt.Errorf("infinicave: cut bounds overflow world coordinates")
		}
	}
	if !finiteCarveValue(faceArea(poly)) || faceArea(poly) <= 1e-15 {
		return rockCut{}, fmt.Errorf("infinicave: cut area is too small or overflows world coordinates")
	}
	cut := rockCut{poly: poly}
	cut.min, cut.max = polygonBounds(poly)
	return cut, nil
}
