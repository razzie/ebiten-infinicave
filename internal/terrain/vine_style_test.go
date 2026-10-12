package terrain

import (
	"image/color"
	"math"
	"math/rand"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func straightStyleVine(x, length float64) Vine {
	vine := Vine{Parent: -1}
	for i := 0; i <= int(length/.0025); i++ {
		vine.Points = append(vine.Points, VinePoint{P: geom.V{X: x, Y: .1 + float64(i)*.0025}, Radius: .003})
	}
	return vine
}

func TestVineDecorationStableWithoutChangingGrowth(t *testing.T) {
	rock := testRockGrid([]geom.V{{X: .5, Y: .5}}, []color.NRGBA{{30, 30, 30, 255}})
	field := newVineTerrain(rock, nil)
	defer field.release()
	makeVines := func() []Vine {
		var vines []Vine
		for i := range 24 {
			length := .8
			if i%2 == 0 {
				length = .35
			}
			vines = append(vines, straightStyleVine(.1+float64(i)*.03, length))
		}
		vines = append(vines, Vine{Parent: 1, Joint: 40, Depth: 1, Points: []VinePoint{{P: vines[1].Points[40].P, Radius: .002}, {P: geom.V{X: .2, Y: .4}, Radius: 0}}})
		return vines
	}
	a, b, original := makeVines(), makeVines(), makeVines()
	decorateVines(field, a)
	decorateVines(field, b)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("decoration is not deterministic")
	}
	hues := map[VineHue]bool{}
	transitions, leaves := 0, 0
	for i, vine := range a {
		if !reflect.DeepEqual(vine.Points, original[i].Points) {
			t.Fatal("decoration changed the growth path")
		}
		if len(vine.Along) != len(vine.Points) {
			t.Fatal("missing material coordinates")
		}
		hues[vine.Style.From] = true
		if vine.Style.From != vine.Style.To {
			transitions++
			if vine.Style.Period < .16 || vine.Style.Period > .22 {
				t.Fatal("generated vine needs frequent material changes")
			}
			if width := vine.Style.End - vine.Style.Start; width < .055 || width > .085 {
				t.Fatal("transition lost its smooth blend")
			}
			length, _ := vine.extent()
			if i < 24 && length >= .7 {
				seen := map[VineHue]bool{}
				for distance := vine.Style.End; distance < length; distance += vine.Style.Period {
					_, hue, _ := vine.Style.Hues(distance)
					seen[hue] = true
				}
				if len(seen) != 2 {
					t.Fatal("long stem does not cycle through both green and yellow")
				}
			}
		}
		for _, leaf := range vine.Leaves {
			leaves++
			if vine.Style.Green(leaf.Along) < .5 || !vineLeafFits(field, leaf) {
				t.Fatal("leaf grew on a non-green or unsupported segment")
			}
		}
	}
	if len(hues) != 2 || transitions == 0 || leaves == 0 {
		t.Fatalf("missing variation: hues=%d transitions=%d leaves=%d", len(hues), transitions, leaves)
	}
	child, parent := a[24], a[1]
	if child.Style != parent.Style || child.Along[0] != parent.Along[child.Joint] {
		t.Fatal("branch did not inherit local parent material")
	}
}

func TestLeavesRespectGuideClearanceAndHue(t *testing.T) {
	rock := testRockGrid([]geom.V{{X: .5, Y: .5}}, []color.NRGBA{{100, 90, 80, 255}})
	guides := []Guide{SplineGuide([]geom.V{{X: .4, Y: GenerationMinY}, {X: .4, Y: generationMaxY}}, 1)}
	field := newForegroundVineTerrain(rock, guides)
	defer field.release()
	vine := straightStyleVine(.44, .8)
	vine.Along = make([]float64, len(vine.Points))
	for i := range vine.Along {
		vine.Along[i] = float64(i) * .0025
	}
	vine.Style = VineStyle{From: VineSage, To: VineStraw, Start: .3, End: .6}
	leaves := generateVineLeaves(field, vine, rand.New(rand.NewSource(42)))
	if len(leaves) == 0 {
		t.Fatal("green stem has no leaves")
	}
	for _, leaf := range leaves {
		if vine.Style.Green(leaf.Along) < .5 {
			t.Fatal("leaves did not thin out through the transition")
		}
		for _, p := range leaf.Polygon {
			if guides[0].project(p).Dist < foregroundVineGuideClearance {
				t.Fatal("leaf crosses a guide exclusion band")
			}
		}
	}
	for _, hue := range []VineHue{VineStraw} {
		vine.Style = VineStyle{From: hue, To: hue}
		if len(generateVineLeaves(field, vine, rand.New(rand.NewSource(42)))) != 0 {
			t.Fatal("non-green stem has leaves")
		}
	}
	// All corners can be supported while the interior crosses the exclusion.
	leaf := VineLeaf{Polygon: [4]geom.V{{X: .35, Y: .3}, {X: .36, Y: .29}, {X: .45, Y: .3}, {X: .36, Y: .31}}}
	for _, p := range leaf.Polygon {
		if field.growthSpace(p) < .0015 {
			t.Fatal("interior-clearance test requires supported corners")
		}
	}
	if vineLeafFits(field, leaf) {
		t.Fatal("leaf spans a guide exclusion band")
	}
}

func TestThinForegroundLeavesAreLarger(t *testing.T) {
	rock := testRockGrid([]geom.V{{X: .5, Y: .5}}, []color.NRGBA{{30, 30, 30, 255}})
	background := newVineTerrain(rock, nil)
	defer background.release()
	foreground := newForegroundVineTerrain(rock, nil)
	defer foreground.release()
	for _, radius := range []float64{.0012, .0022} {
		vine := straightStyleVine(.5, .8)
		vine.Along = make([]float64, len(vine.Points))
		for i := range vine.Points {
			vine.Points[i].Radius = radius
			vine.Along[i] = float64(i) * .0025
		}
		vine.Style = VineStyle{From: VineSage, To: VineSage}
		back := generateVineLeaves(background, vine, rand.New(rand.NewSource(42)))
		front := generateVineLeaves(foreground, vine, rand.New(rand.NewSource(42)))
		if len(back) == 0 || len(back) != len(front) {
			t.Fatal("leaf sizing changed attachment frequency on open rock")
		}
		for i, leaf := range front {
			backLength := back[i].Polygon[2].Sub(back[i].Polygon[0]).Len()
			frontLength := leaf.Polygon[2].Sub(leaf.Polygon[0]).Len()
			if radius == .0012 && frontLength < backLength*1.2 {
				t.Fatal("thin foreground stem did not receive larger leaves")
			}
			if radius == .0022 && math.Abs(frontLength-backLength) > 1e-12 {
				t.Fatal("leaf sizing changed on a broader stem")
			}
		}
	}
}

func TestCarvingPreservesVineDistancesAndLeafAttachments(t *testing.T) {
	vine := straightStyleVine(.5, .8)
	vine.Foreground = true
	vine.Style = VineStyle{From: VineSage, To: VineStraw, Start: .03, End: .10, Period: .18}
	vine.Along = make([]float64, len(vine.Points))
	for i := range vine.Along {
		vine.Along[i] = float64(i) * .0025
	}
	for _, along := range []float64{.1, .4, .7} {
		p := geom.V{X: .5, Y: .1 + along}
		vine.Leaves = append(vine.Leaves, VineLeaf{Anchor: p, Along: along, Polygon: [4]geom.V{p, p.Add(geom.V{X: .02, Y: -.01}), p.Add(geom.V{X: .04}), p.Add(geom.V{X: .02, Y: .01})}})
	}
	original := []Vine{vine}
	cut, _ := CutFromHole(Hole{Shape: HoleCircle, Center: geom.V{X: .5, Y: -.5}, Radius: .08})
	vines, hit := cut.Vines(original, -1)
	if !hit || len(vines) != 2 {
		t.Fatal("vine did not split")
	}
	for _, v := range vines {
		if v.Style != vine.Style || len(v.Leaves) != 1 {
			t.Fatal("cut changed style or kept a detached leaf")
		}
		for i, p := range v.Points {
			if math.Abs(v.Along[i]-(p.P.Y-.1)) > 1e-12 {
				t.Fatal("material distance restarted after cutting")
			}
		}
	}
	if vines[0].Leaves[0] != vine.Leaves[0] || vines[1].Leaves[0] != vine.Leaves[2] {
		t.Fatal("surviving leaves moved")
	}
	// A cut touching only a leaf still needs a render mask.
	leafCut, _ := CutFromHole(Hole{Shape: HoleCircle, Center: geom.V{X: .536, Y: -.8}, Radius: .004})
	plants, changed := leafCut.vegetation(Vegetation{Vines: original}, -1)
	if !changed || len(plants.Cuts) != 1 || len(plants.Vines) != 1 || len(plants.Vines[0].Leaves) != 3 {
		t.Fatal("leaf-only damage failed to retain the render mask")
	}
	if len(original[0].Leaves) != 3 || original[0].Along[0] != 0 {
		t.Fatal("cut mutated the original snapshot")
	}
	// Repeated splits keep absolute coordinates and the remaining attachment.
	second, _ := CutFromHole(Hole{Shape: HoleCircle, Center: geom.V{X: .5, Y: -.7}, Radius: .03})
	vines, _ = second.Vines(vines, -1)
	for _, v := range vines {
		for i, p := range v.Points {
			if math.Abs(v.Along[i]-(p.P.Y-.1)) > 1e-12 {
				t.Fatal("repeated cut moved the transition")
			}
		}
	}
}

func TestHorizontalSectionTransformsLeavesWithTheirStems(t *testing.T) {
	anchor := geom.V{X: .3, Y: .4}
	leaf := VineLeaf{Anchor: anchor, Along: .2, Polygon: [4]geom.V{anchor, anchor.Add(geom.V{X: .01, Y: .02}), anchor.Add(geom.V{X: .03, Y: .04}), anchor.Add(geom.V{X: .02, Y: .01})}}
	style := VineStyle{From: VineSage, To: VineStraw, Start: .1, End: .5}
	vine := Vine{Parent: -1, Style: style, Along: []float64{.2, .3}, Leaves: []VineLeaf{leaf}, Points: []VinePoint{{P: anchor, Radius: .002}, {P: anchor.Add(geom.V{Y: .1}), Radius: 0}}}
	section := OrientedSection(Horizontal, Section{Vines: []Vine{vine}})
	got := section.Vines[0]
	if got.Leaves[0].Anchor != got.Points[0].P || got.Leaves[0].Anchor != Horizontal.local(anchor) {
		t.Fatal("rotated leaf lost its stem attachment")
	}
	for i, p := range leaf.Polygon {
		if got.Leaves[0].Polygon[i] != Horizontal.local(p) {
			t.Fatal("leaf geometry did not rotate with its stem")
		}
	}
	if got.Style != style || got.Leaves[0].Along != leaf.Along || !reflect.DeepEqual(got.Along, []float64{.2, .3}) {
		t.Fatal("rotation changed material distances")
	}
}

func TestRepeatingVineMaterialIsContinuousAndLeavesFollowGreen(t *testing.T) {
	style := VineStyle{From: VineSage, To: VineStraw, Start: .03, End: .10, Period: .18}
	for cycle := range 9 {
		start := style.Start + float64(cycle)*style.Period
		beforeFrom, beforeTo, beforeBlend := style.Hues(start - 1e-9)
		afterFrom, _, afterBlend := style.Hues(start)
		previous := beforeFrom
		if beforeBlend == 1 {
			previous = beforeTo
		}
		if previous != afterFrom || afterBlend != 0 {
			t.Fatal("repeating material jumps at a cycle boundary")
		}
		_, hue, blend := style.Hues(style.End + float64(cycle)*style.Period + 1e-9)
		if blend != 1 {
			t.Fatal("transition does not reach a stable hue")
		}
		wantGreen := 0.0
		if hue == VineSage {
			wantGreen = 1
		}
		if got := style.Green(style.End + float64(cycle)*style.Period + 1e-9); got != wantGreen {
			t.Fatal("leaf eligibility does not follow repeated green segments")
		}
	}
}

func TestVineColorsOnlyUseGreenAndYellow(t *testing.T) {
	for _, period := range []float64{0, .18} {
		for _, from := range []VineHue{VineSage, VineStraw} {
			for _, to := range []VineHue{VineSage, VineStraw} {
				style := VineStyle{From: from, To: to, Start: .03, End: .11, Period: period}
				for sample := -10; sample < 2000; sample++ {
					a, b, _ := style.Hues(float64(sample) * .001)
					if a > VineStraw || b > VineStraw {
						t.Fatal("vine selected a color other than green or yellow")
					}
				}
			}
		}
	}
}
