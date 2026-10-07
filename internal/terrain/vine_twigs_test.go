package terrain

import (
	"image/color"
	"math"
	"math/rand"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestTinyVineBranchesFollowExactCellEdges(t *testing.T) {
	var seeds []geom.V
	for y := 0.2; y <= 1.2; y += 0.04 {
		seeds = append(seeds, geom.V{X: 0.47, Y: y}, geom.V{X: 0.53, Y: y})
	}
	background := newRockGrid(seeds, func(geom.V) color.NRGBA { return color.NRGBA{30, 30, 30, 255} })
	// This raised face blocks the right side of the seam, while left-hand
	// branches have open rock. The root is slightly off-seam to test joining.
	foreground := RockGrid{{Polygon: []geom.V{{X: 0.52, Y: GenerationMinY}, {X: generationWidth, Y: GenerationMinY}, {X: generationWidth, Y: generationMaxY}, {X: 0.52, Y: generationMaxY}}, Color: color.NRGBA{180, 180, 170, 255}}}
	field := newVineTerrain(background, foreground)
	parent := Vine{Parent: -1}
	for y := 0.25; y <= 1; y += 0.0025 {
		parent.Points = append(parent.Points, VinePoint{geom.V{X: 0.498, Y: y}, 0.002})
	}
	generate := func(seed int64) []Vine {
		return addVineEdgeBranches(field, []Vine{parent}, rand.New(rand.NewSource(seed)))
	}
	vines := generate(42)
	if len(vines) < 7 {
		t.Fatalf("expected many tiny branches, got %d", len(vines)-1)
	}
	if !reflect.DeepEqual(vines[0], parent) {
		t.Fatal("adding fine branches changed the parent")
	}
	if !reflect.DeepEqual(vines, generate(42)) || reflect.DeepEqual(vines, generate(43)) {
		t.Fatal("tiny branches must reproduce with the seed and vary across seeds")
	}
	// Measure against the source polygons, independently of the growth graph
	// and raster field. Check segment midpoints too, to catch corner shortcuts.
	distanceToEdge := func(p geom.V) float64 {
		distance := math.Inf(1)
		for _, cell := range background {
			for i, a := range cell.Polygon {
				b := cell.Polygon[(i+1)%len(cell.Polygon)]
				d := b.Sub(a)
				if d.Len2() > 0 {
					q := a.Add(d.Mul(geom.Clamp(p.Sub(a).Dot(d)/d.Len2(), 0, 1)))
					distance = math.Min(distance, p.Sub(q).Len())
				}
			}
		}
		return distance
	}
	turns := 0
	for _, v := range vines[1:] {
		length, _ := v.extent()
		if !v.EdgeAligned || v.Parent != 0 || v.Depth != 1 || v.Points[0].P != parent.Points[v.Joint].P {
			t.Fatal("fine branch has an invalid attachment")
		}
		if length < vineMinEdgeBranchLength || length > .068+1e-9 || v.Points[0].Radius > .00095 || v.Points[len(v.Points)-1].Radius != 0 {
			t.Fatalf("branch is not tiny and tapered: length %.2f", length)
		}
		along, onSeam := 0.0, false
		for i, point := range v.Points {
			if field.growthSpace(point.P) < point.Radius || point.P.X+point.Radius > .520+vineForegroundTouch {
				t.Fatal("tiny branch entered raised rock")
			}
			if i == 0 {
				continue // The exact parent attachment precedes the seam connector.
			}
			previous := v.Points[i-1]
			along += point.P.Sub(previous.P).Len()
			for u := 0.0; u <= 1; u += .25 {
				if field.growthSpace(geom.LerpVector(previous.P, point.P, u)) < geom.Lerp(previous.Radius, point.Radius, u) {
					t.Fatal("fine ribbon crosses unsupported terrain")
				}
			}
			if distanceToEdge(point.P) > 1e-9 && (onSeam || along > .006) {
				t.Fatalf("branch leaves polygon edge at %v", point.P)
			}
			if onSeam && distanceToEdge(geom.LerpVector(previous.P, point.P, .5)) > 1e-9 {
				t.Fatal("branch cuts across a cell corner")
			}
			onSeam = distanceToEdge(point.P) <= 1e-9
			if i > 1 {
				a := v.Points[i-1].P.Sub(v.Points[i-2].P).Norm()
				b := point.P.Sub(v.Points[i-1].P).Norm()
				if a.Dot(b) < .9 {
					turns++
				}
			}
		}
	}
	if turns < 5 {
		t.Fatal("fine branches did not follow cell junctions")
	}
}

func TestTinyVineBranchesNeedSupportedSeams(t *testing.T) {
	parent := Vine{Parent: -1}
	for y := 0.3; y <= 0.7; y += 0.0025 {
		parent.Points = append(parent.Points, VinePoint{geom.V{X: 0.498, Y: y}, 0.002})
	}
	dark := color.NRGBA{30, 30, 30, 255}
	background := testRockGrid([]geom.V{{X: 0.47, Y: 0.5}, {X: 0.53, Y: 0.5}}, []color.NRGBA{dark, dark})
	for _, field := range []*VineTerrain{
		newVineTerrain(testRockGrid([]geom.V{{X: 0.5, Y: 0.5}}, []color.NRGBA{dark}), nil),
		newVineTerrain(background, background),
		newVineTerrain(nil, nil),
	} {
		if got := addVineEdgeBranches(field, []Vine{parent}, rand.New(rand.NewSource(42))); !reflect.DeepEqual(got, []Vine{parent}) {
			t.Fatal("branches grew without exposed, supporting seams")
		}
	}
}
