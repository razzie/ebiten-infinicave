package main

import (
	"image/color"
	"math"
	"math/rand"
	"reflect"
	"testing"
)

func testRockGrid(seeds []V, tones []color.NRGBA) RockGrid {
	return newRockGrid(seeds, func(p V) color.NRGBA {
		for i, seed := range seeds {
			if p == seed {
				return tones[i]
			}
		}
		panic("unknown seed")
	})
}

func TestVineTerrainReadsBothVoronoiGrids(t *testing.T) {
	// The background has a vertical boundary; the foreground has a horizontal
	// one. Transparent foreground must preserve the underlying dark/void split.
	background := testRockGrid([]V{{250, 500}, {750, 500}}, []color.NRGBA{{30, 30, 30, 255}, {0, 0, 0, 255}})
	foreground := testRockGrid([]V{{500, 250}, {500, 750}}, []color.NRGBA{{170, 170, 160, 255}, {}})
	field := newVineTerrain(background, foreground)
	for _, p := range []V{{250, 250}, {750, 250}, {750, 750}} {
		if field.space(p) != 0 {
			t.Errorf("vine can enter light rock or void at %v", p)
		}
	}
	if field.space(V{250, 750}) < 100 {
		t.Fatal("transparent foreground hides traversable background")
	}
	if field.space(V{495, 750}) > 5 || field.space(V{250, 505}) > 5 {
		t.Fatal("clearance ignores a grid boundary")
	}

	// A dark translucent foreground face can support a vine over black, but
	// a faint overlay is not an opaque wall hiding the underlying dark grid.
	foreground = testRockGrid([]V{{500, 500}}, []color.NRGBA{{50, 50, 50, 128}})
	field = newVineTerrain(background, foreground)
	if field.space(V{750, 750}) < 100 {
		t.Fatal("visible foreground rock cannot support vines")
	}
	foreground = testRockGrid([]V{{500, 500}}, []color.NRGBA{{170, 170, 170, 32}})
	field = newVineTerrain(background, foreground)
	if field.space(V{250, 750}) < 100 {
		t.Fatal("faint light overlay incorrectly blocks dark rock")
	}
}

func TestVinesBranchCurlAndStayOnDarkFaces(t *testing.T) {
	background := testRockGrid([]V{{500, 500}}, []color.NRGBA{{30, 30, 30, 255}})
	// A vertical light barrier divides two large dark growth regions.
	foreground := testRockGrid([]V{{100, 500}, {500, 500}, {900, 500}}, []color.NRGBA{{}, {180, 180, 170, 255}, {}})
	field := newVineTerrain(background, foreground)
	vines := generateVines(field, rand.New(rand.NewSource(42)))
	forks, curls, longTrunks := 0, 0, 0
	for vineIndex, vine := range vines {
		length, span := vine.extent()
		if vine.Depth == 0 {
			if length < vineMinTrunkLength || span < vineMinTrunkSpan {
				t.Fatalf("isolated short vine: length %.1f, span %.1f", length, span)
			}
			if length > 500 {
				longTrunks++
			}
		} else {
			minimum := vineMinBranchLength
			if vine.Depth == 2 {
				minimum = vineMinTwigLength
			}
			if length < minimum {
				t.Fatalf("short blocked offshoot: %.1f", length)
			}
		}
		if vine.Points[len(vine.Points)-1].Radius != 0 {
			t.Fatal("blunt vine tip")
		}
		turn := 0.0
		for i, p := range vine.Points {
			// Check against the known Voronoi boundaries independently of the
			// sampled clearance used by growth, including the ribbon edges.
			if p.P.X+p.Radius >= 300 && p.P.X-p.Radius <= 700 {
				t.Fatalf("vine overlaps the light face at %+v", p)
			}
			if math.IsNaN(p.Radius) || math.IsNaN(p.P.X) || field.space(p.P) < p.Radius {
				t.Fatalf("vine leaves dark terrain at %+v", p)
			}
			if i > 0 {
				a := vine.Points[i-1]
				for u := 0.0; u <= 1; u += .25 {
					if field.space(lerpV(a.P, p.P, u)) < lerp(a.Radius, p.Radius, u) {
						t.Fatal("vine width crosses light barrier")
					}
				}
			}
			if i >= 2 {
				a := vine.Points[i-1].P.Sub(vine.Points[i-2].P).Norm()
				b := p.P.Sub(vine.Points[i-1].P).Norm()
				turn += math.Atan2(a.X*b.Y-a.Y*b.X, a.Dot(b))
			}
		}
		if math.Abs(turn) > math.Pi*.75 {
			curls++
		}
		if vine.Depth == 0 {
			continue
		}
		forks++
		if vine.Parent < 0 || vine.Parent >= vineIndex {
			t.Fatal("branch has no preceding parent")
		}
		parent := vines[vine.Parent]
		if parent.Depth != vine.Depth-1 || vine.Joint < 0 || vine.Joint >= len(parent.Points) {
			t.Fatal("invalid parent attachment")
		}
		attachment := parent.Points[vine.Joint]
		if attachment.P != vine.Points[0].P || attachment.Radius < vine.Points[0].Radius {
			t.Fatal("detached branch")
		}
	}
	if longTrunks < 2 {
		t.Fatalf("expected long trunks on both sides of the barrier, got %d", longTrunks)
	}
	if forks < 3 || curls == 0 {
		t.Fatalf("expected branching curls, got %d forks and %d curls", forks, curls)
	}
	a := generateVines(field, rand.New(rand.NewSource(42)))
	b := generateVines(field, rand.New(rand.NewSource(43)))
	if !reflect.DeepEqual(vines, a) || reflect.DeepEqual(vines, b) {
		t.Fatal("seeded vines must reproduce and vary with a new seed")
	}
}

func TestVinesHandleBlockedTerrain(t *testing.T) {
	field := newVineTerrain(nil, nil)
	if vines := generateVines(field, rand.New(rand.NewSource(42))); len(vines) != 0 {
		t.Fatal("vines grew without supporting rocks")
	}
	vine := growVine(field, V{500, 500}, V{1, 0}, 6, 100, 0, 1, 0)
	if len(vine.Points) != 1 || vine.Points[0].Radius != 0 {
		t.Fatal("blocked growth must stop with a finite zero-width tip")
	}
}

func TestVinesSkipSmallIsolatedRockPatches(t *testing.T) {
	// Four neighboring seeds enclose a 140 x 140 dark Voronoi face. It can
	// support a short vine, but should be left empty instead of producing a
	// disconnected miniature plant (or a long spiral in the same few cells).
	background := testRockGrid(
		[]V{{500, 500}, {360, 500}, {640, 500}, {500, 360}, {500, 640}},
		[]color.NRGBA{{30, 30, 30, 255}, {0, 0, 0, 255}, {0, 0, 0, 255}, {0, 0, 0, 255}, {0, 0, 0, 255}},
	)
	field := newVineTerrain(background, nil)
	if field.space(V{500, 500}) < 30 {
		t.Fatal("fixture must support vine growth")
	}
	for _, seed := range []int64{42, 123, 789} {
		if vines := generateVines(field, rand.New(rand.NewSource(seed))); len(vines) != 0 {
			t.Fatalf("seed %d populated a small isolated rock patch with %d vines", seed, len(vines))
		}
	}
}

func TestTrunkJoinsAndBlockedHalfTaper(t *testing.T) {
	field := newVineTerrain(testRockGrid([]V{{500, 500}}, []color.NRGBA{{30, 30, 30, 255}}), nil)
	root := V{500, 500}
	a := growVine(field, root, V{1, 0}, 8, 160, 0, 1, 0)
	b := growVine(field, root, V{-1, 0}, 8, 160, 0, -1, 0)
	blocked := Vine{Points: []VinePoint{{root, 0}}, Parent: -1}
	for _, halves := range [][2]Vine{{a, b}, {a, blocked}, {blocked, b}, {blocked, blocked}} {
		vine, joint := joinVineHalves(halves[0], halves[1])
		if vine.Points[joint].P != root {
			t.Fatal("joined trunk lost its shared root")
		}
		if vine.Points[0].Radius != 0 || vine.Points[len(vine.Points)-1].Radius != 0 {
			t.Fatal("joined trunk has a blunt, disconnected-looking endpoint")
		}
		for i, p := range vine.Points {
			if i > 0 && p.P.Sub(vine.Points[i-1].P).Len() > 2.500001 {
				t.Fatal("gap in trunk centerline")
			}
			if math.IsNaN(p.Radius) || p.Radius < 0 {
				t.Fatal("invalid trunk width")
			}
		}
		if len(halves[0].Points) > 1 && len(halves[1].Points) > 1 && vine.Points[joint].Radius != 8 {
			t.Fatal("interior trunk join is pinched")
		}
		// A blocked half needs a gradual taper on the surviving side.
		if len(halves[0].Points) == 1 && len(halves[1].Points) > 1 && vine.Points[joint-1].Radius >= 1 {
			t.Fatal("blocked forward half leaves an abrupt tip")
		}
		if len(halves[1].Points) == 1 && len(halves[0].Points) > 1 && vine.Points[joint+1].Radius >= 1 {
			t.Fatal("blocked backward half leaves an abrupt tip")
		}
	}
}

func TestForkShadingMeetsParent(t *testing.T) {
	parent := Vine{Parent: -1, Points: []VinePoint{{V{100, 80}, 8}, {V{100, 100}, 8}, {V{100, 120}, 8}}}
	branch := Vine{Depth: 1, Parent: 0, Joint: 1, Points: []VinePoint{{V{100, 100}, 5}, {V{130, 160}, 0}}}
	vines := []Vine{parent, branch}
	// Even the dark outer bands of the child inherit the red parent face at
	// the join instead of drawing a black cut through its highlight.
	for _, x := range []float64{96, 100, 104} {
		p := V{x, 100}
		want := vineBandColor((100 - x) / 8)
		got := vineJoinColor(vines, 1, p, vineColors[0])
		if got != want {
			t.Fatalf("fork seam at %v: got %v, want parent material %v", p, got, want)
		}
	}
	if got := vineJoinColor(vines, 1, V{130, 160}, vineColors[3]); got != vineColors[3] {
		t.Fatal("parent shading extends beyond the fork")
	}
}
