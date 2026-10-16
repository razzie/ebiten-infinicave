package terrain

import (
	"context"
	"image/color"
	"math"
	"math/rand"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestForegroundFamiliesKeepFineAttachedBranches(t *testing.T) {
	rock := testRockGrid([]geom.V{{X: .5, Y: .5}}, []color.NRGBA{{30, 30, 30, 255}})
	for _, foreground := range []bool{false, true} {
		field := newVineTerrain(rock, nil)
		if foreground {
			field.release()
			field = newForegroundVineTerrain(rock, nil)
		}
		vines := generateVinesInBandContext(context.Background(), field, rand.New(rand.NewSource(42)), 0, SectionHeight, 1)
		field.release()
		main, secondary := 0, 0
		maxRadius := vineMaxRadius
		if foreground {
			maxRadius = .0015
		}
		for i, vine := range vines {
			for _, p := range vine.Points {
				if p.Radius > maxRadius {
					t.Fatal("fine vine family grew a broad stem")
				}
				if foreground && vine.Depth > 0 && p.Radius > maxRadius*.55 {
					t.Fatal("fine branches did not taper below their trunk width")
				}
			}
			if vine.EdgeAligned || vine.Parent < 0 {
				continue
			}
			if vine.Depth == 1 {
				main++
			} else {
				secondary++
			}
			if vine.Parent >= i || vine.Points[0].P != vines[vine.Parent].Points[vine.Joint].P {
				t.Fatal("branch detached from its attachment")
			}
		}
		if foreground {
			if main < 2 || main > 4 || secondary > main {
				t.Fatalf("foreground needs a small attached family: main=%d secondary=%d", main, secondary)
			}
		} else if main < 1 || main > 3 || secondary > 2 {
			t.Fatalf("background kept dense branching: main=%d secondary=%d", main, secondary)
		}
	}
}

func TestCenterVinesOccasionallyFollowGuidesAndRarelyCoverTheirLength(t *testing.T) {
	for _, mode := range []Orientation{Vertical, Horizontal} {
		knots := []geom.V{{X: .15, Y: .25}, {X: .5, Y: .27}, {X: .85, Y: .25}}
		MapPoints(knots, mode.internalLocal)
		guide := SplineGuide(knots, 1)
		guide.faceUp(mode)
		guide.Seed = 42
		ground := RockGrid{terrainRect(.1, .2, .8, .10), terrainRect(.1, .3, .8, .5)}
		up := InternalPoint(mode, geom.V{Y: -.6})
		ground[0].Normal = geom.V3{X: up.X, Y: up.Y, Z: .8}
		for i := range ground {
			MapPoints(ground[i].Polygon, mode.internalLocal)
			ground[i].Center = mode.internalLocal(ground[i].Center)
			ground[i].orientation = mode
		}
		support := newPathSupport(NewRockTopology(ground), []Guide{guide})
		guided := support.forRoute(GuideVineRoute)
		stem := Vine{Parent: -1, Route: CellCenterVineRoute, Foreground: true}
		for i := 0; i <= 20; i++ {
			t := float64(i) / 20
			stem.Points = append(stem.Points, VinePoint{P: mode.internalLocal(geom.V{X: .4 + .1*t, Y: .45 - .14*t}), Radius: .0013})
		}
		stem.Points[0].Radius, stem.Points[20].Radius = 0, 0
		none, partial, full := 0, 0, 0
		for seed := int64(1); seed <= 200; seed++ {
			vines := addGuideVineBranches([]Vine{stem}, &support, rand.New(rand.NewSource(seed)))
			if !reflect.DeepEqual(vines, addGuideVineBranches([]Vine{stem}, &support, rand.New(rand.NewSource(seed)))) {
				t.Fatal("guide runners are not deterministic")
			}
			if len(vines) == 1 {
				none++
				continue
			}
			decoratePathVines(vines, &support, seed)
			lo, hi := math.Inf(1), math.Inf(-1)
			for _, v := range vines[1:] {
				if v.Route != GuideVineRoute || v.Parent != 0 || v.Points[0].P != stem.Points[v.Joint].P || v.Points[0].Radius > stem.Points[v.Joint].Radius || v.Points[len(v.Points)-1].Radius != 0 {
					t.Fatal("guide runner lost its route, attachment, or taper")
				}
				if v.Style != vines[0].Style || v.Along[0] != vines[0].Along[v.Joint] {
					t.Fatal("guide continuation lost its parent's material")
				}
				for j, point := range v.Points {
					pr := guide.project(point.P)
					lo, hi = min(lo, pr.S), max(hi, pr.S)
					if pr.Signed <= 0 {
						t.Fatal("guide runner grew above its crest")
					}
					if j > 0 && !guided.fits(v.Points[j-1].P, point.P, max(point.Radius, v.Points[j-1].Radius)) {
						t.Fatal("guide runner left its supporting rock")
					}
				}
				// Following the guide comes closer than ordinary cell routes may.
				tip := guide.project(v.Points[len(v.Points)-1].P)
				if tip.Dist < .03 || tip.Dist > .06 || WorldPoint(mode, v.Points[len(v.Points)-1].P).Y < .3015 {
					t.Fatalf("guide continuation did not stay slightly below the guide: %g", tip.Dist)
				}
			}
			if lo < 1e-9 && hi > guide.S[len(guide.S)-1]-1e-9 {
				full++
			} else {
				partial++
			}
		}
		if none < 100 || partial == 0 || full == 0 || full >= partial/3 {
			t.Fatalf("mode %v: guide runs have wrong occurrence: none=%d partial=%d full=%d", mode, none, partial, full)
		}
	}
}

func TestGuideVinesStopAtMissingRock(t *testing.T) {
	guide := SplineGuide([]geom.V{{X: .15, Y: .25}, {X: .85, Y: .25}}, 1)
	ground := RockGrid{terrainRect(.1, .2, .38, .5), terrainRect(.52, .2, .38, .5)}
	support := newPathSupport(NewRockTopology(ground), []Guide{guide}).forRoute(GuideVineRoute)
	stem := Vine{Parent: -1, Route: CellCenterVineRoute, Foreground: true, Points: []VinePoint{
		{P: geom.V{X: .27, Y: .33}, Radius: .0013},
		{P: geom.V{X: .3, Y: .30}, Radius: .0013},
		{P: geom.V{X: .33, Y: .28}, Radius: 0},
	}}
	v := traceGuideVine(stem, 1, &guide, .15, guide.S[len(guide.S)-1], 0, &support)
	if len(v.Points) < 3 {
		t.Fatal("guide runner was discarded instead of stopping at the gap")
	}
	for _, p := range v.Points {
		if p.P.X >= .48 {
			t.Fatal("guide runner crossed an unsupported gap")
		}
	}
	if v.Points[len(v.Points)-1].Radius != 0 {
		t.Fatal("blocked guide runner has a blunt tip")
	}
}

func TestStreamedCenterVinesCanReachGuides(t *testing.T) {
	for _, mode := range []Orientation{Vertical, Horizontal} {
		runners := 0
		// Excluding top-facing cells leaves fewer eligible guide stretches.
		for _, seed := range []int64{1, 7, 42, 93, 12345, 55, 72, 1024} {
			data := NewSectionBuilder(seed, nil, mode).Build(0)
			for _, v := range data.ForegroundVines {
				if v.Route != GuideVineRoute {
					continue
				}
				runners++
				if v.Parent < 0 || data.ForegroundVines[v.Parent].Route != CellCenterVineRoute {
					t.Fatal("guide runner did not continue a cell center vine")
				}
			}
		}
		if runners == 0 {
			t.Fatalf("mode %v: streamed center vines never reached a guide", mode)
		}
	}
}
