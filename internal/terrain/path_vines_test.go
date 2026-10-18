package terrain

import (
	"context"
	"math"
	"math/rand"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func BenchmarkBackgroundPathVines(b *testing.B) {
	build := NewSectionBuilder(42, nil).Begin(context.Background(), 0)
	if !build.Materialize(context.Background()) {
		b.Fatal("materialization failed")
	}
	footprint := newRockFootprint(build.raw)
	footprint.background = newRockFootprint(build.data.Background)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		backgroundPathVines(42, 0, Vertical, footprint)
	}
}

func BenchmarkForegroundVineCellLimit(b *testing.B) {
	data := NewSectionBuilder(42, nil).Build(0)
	support := newPathSupport(data.ForegroundBake, data.Guides)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		limitVinesPerCell(data.ForegroundVines, &support)
	}
}

func TestVineCellLimitCountsStemsAndPreservesAttachments(t *testing.T) {
	support := newPathSupport(NewRockTopology(RockGrid{
		terrainRect(.1, .1, .3, .3), terrainRect(.6, .1, .3, .3),
	}), nil)
	stem := func(x, y float64, parent int) Vine {
		return Vine{Parent: parent, Foreground: true, Points: []VinePoint{
			{P: geom.V{X: x, Y: y}, Radius: .001},
			{P: geom.V{X: x + .02, Y: y + .02}, Radius: .001},
			{P: geom.V{X: x, Y: y + .04}},
		}}
	}
	vines := []Vine{
		stem(.2, .2, -1), stem(.2, .2, 0), // Parent and fork fill the left cell.
		stem(.7, .2, -1), // Rejected after visiting both cells; commits neither.
		stem(.7, .2, 2),  // Descendant of the rejected stem.
		stem(.7, .2, -1), stem(.7, .2, 4),
	}
	vines[2].Points = append(vines[2].Points, vines[0].Points[0])
	vines[4].Along, vines[4].styleFamily, vines[4].styleSet = []float64{0, .03, .06}, 4, true
	vines[5].styleFamily, vines[5].styleSet = 4, true
	kept := limitVinesPerCell(vines, &support)
	if len(kept) != 4 || kept[1].Parent != 0 || kept[2].Parent != -1 || kept[3].Parent != 2 {
		t.Fatalf("limit damaged surviving attachments: %+v", kept)
	}
	if !reflect.DeepEqual(kept[2].Along, vines[4].Along) || VineStyleFamily(kept, 2) != 4 || VineStyleFamily(kept, 3) != 4 {
		t.Fatal("limit changed baked material distances or identities")
	}
	if vines[5].Parent != 4 || !reflect.DeepEqual(kept, limitVinesPerCell(vines, &support)) {
		t.Fatal("limit changed the input or was not deterministic")
	}
	for _, v := range kept {
		if v.Parent >= 0 && v.Points[0].P != kept[v.Parent].Points[v.Joint].P {
			t.Fatal("surviving fork is detached")
		}
	}
}

func TestVineCellLimitIncludesSparseRibbonsWidthAndLeaves(t *testing.T) {
	support := newPathSupport(NewRockTopology(RockGrid{terrainRect(.4, .4, .1, .1)}), nil)
	stem := func(a, b geom.V, radius float64) Vine {
		return Vine{Parent: -1, Points: []VinePoint{{P: a, Radius: radius}, {P: b, Radius: radius}}}
	}
	base := stem(geom.V{X: .41, Y: .45}, geom.V{X: .49, Y: .45}, .001)
	leaf := stem(geom.V{X: .3, Y: .45}, geom.V{X: .31, Y: .45}, .001)
	leaf.Leaves = []VineLeaf{{Polygon: [4]geom.V{
		{X: .3, Y: .45}, {X: .45, Y: .3}, {X: .6, Y: .45}, {X: .45, Y: .6},
	}}}
	for _, candidate := range []Vine{
		stem(geom.V{X: .3, Y: .45}, geom.V{X: .6, Y: .45}, .001),
		stem(geom.V{X: .3, Y: .398}, geom.V{X: .6, Y: .398}, .003),
		leaf, // The leaf completely encloses the cell without crossing its edges.
	} {
		if got := limitVinesPerCell([]Vine{base, base, candidate}, &support); len(got) != 2 {
			t.Fatal("third vine covered a cell between samples, with its width, or with a leaf")
		}
	}
	// Transparent rock and nearby geometry must not consume a cell's slots.
	support.grid[0].Color.A = 0
	if got := limitVinesPerCell([]Vine{base, base, base}, &support); len(got) != 3 {
		t.Fatal("transparent cell consumed coverage")
	}
}

func TestVineCellLimitUsesCellOwnershipAtSeams(t *testing.T) {
	// Each cell straddles a section edge. Only its center decides ownership;
	// whole vines can still cross the seam within their owned cell.
	support := newPathSupport(NewRockTopology(RockGrid{
		terrainRect(.1, -.02, .2, .06), terrainRect(.5, -.04, .2, .06),
	}), nil)
	vine := Vine{Parent: -1, Points: []VinePoint{
		{P: geom.V{X: .2, Y: -.01}, Radius: .001}, {P: geom.V{X: .2, Y: .02}},
	}}
	foreign := vine
	foreign.Points = []VinePoint{{P: geom.V{X: .6, Y: .01}, Radius: .001}, {P: geom.V{X: .6, Y: -.02}}}
	kept := limitVinesPerCell([]Vine{foreign, vine}, &support)
	if len(kept) != 1 || !reflect.DeepEqual(kept[0], vine) {
		t.Fatal("seam cell received a vine from another owner or lost its complete owned vine")
	}
}

func TestForegroundVineCellCoverageAcrossSections(t *testing.T) {
	for _, mode := range []Orientation{Vertical, Horizontal} {
		for _, seed := range []int64{7, 42} {
			builder := NewSectionBuilder(seed, nil, mode)
			var sections []SectionData
			for id := int64(0); id < 3; id++ {
				sections = append(sections, builder.Build(id))
			}
			reloaded := NewSectionBuilder(seed, nil, mode).Build(1)
			if !reflect.DeepEqual(sections[1].ForegroundVines, reloaded.ForegroundVines) {
				t.Fatalf("mode %v seed %d: coverage changed on reload", mode, seed)
			}
			support := newPathSupport(sections[1].ForegroundBake, nil)
			counts := make([]int, len(support.grid))
			for _, section := range sections {
				for _, v := range section.ForegroundVines {
					v.Points = append([]VinePoint(nil), v.Points...)
					v.Leaves = append([]VineLeaf(nil), v.Leaves...)
					delta := geom.V{Y: SectionTop(section.ID) - SectionTop(sections[1].ID)}
					for j := range v.Points {
						v.Points[j].P = v.Points[j].P.Add(delta)
					}
					for j := range v.Leaves {
						for k := range v.Leaves[j].Polygon {
							v.Leaves[j].Polygon[k] = v.Leaves[j].Polygon[k].Add(delta)
						}
					}
					seen := make(map[int]bool)
					support.visitVineCells(v, func(cell int) bool {
						if !seen[cell] {
							seen[cell] = true
							counts[cell]++
						}
						return true
					})
				}
			}
			for i, n := range counts {
				if support.grid[i].Center.Y >= 0 && support.grid[i].Center.Y < SectionHeight && n > 2 {
					t.Errorf("mode %v seed %d cell %v covered by %d vines", mode, seed, support.grid[i].Center, n)
				}
			}
		}
	}
}

func TestBackgroundVinesPreferExposedPadding(t *testing.T) {
	// Seed 42's previous dense layout had these outer-padding lengths.
	for _, tc := range []struct {
		mode          Orientation
		previousOuter float64
	}{{Vertical, 60.39}, {Horizontal, 54.74}} {
		mode := tc.mode
		build := NewSectionBuilder(42, nil, mode).Begin(context.Background(), 0)
		if !build.Materialize(context.Background()) {
			t.Fatal("materialization failed")
		}
		footprint := newRockFootprint(build.raw)
		footprint.background = newRockFootprint(build.data.Background)
		vines := build.data.Vines
		if !reflect.DeepEqual(vines, backgroundPathVines(42, 0, mode, footprint)) {
			t.Fatal("padding steering changed on reload")
		}
		outer, middle, connected := 0.0, 0.0, 0
		lanes := [2][3]float64{}
		for i, v := range vines {
			family := VineFamily(vines, i)
			root := vines[family].Points[0].P
			fromPadding := root.X <= backgroundSolidBorderWidth || root.X >= generationWidth-backgroundSolidBorderWidth
			if !fromPadding {
				t.Fatalf("%s: vine family spawned in the middle at %v", mode, root)
			}
			reachesMiddle := false
			for j := 1; j < len(v.Points); j++ {
				a, b := v.Points[j-1], v.Points[j]
				mid := geom.LerpVector(a.P, b.P, .5)
				length := b.P.Sub(a.P).Len()
				if mid.X < 0 || mid.X > generationWidth {
					outer += length
					x, side := mid.X, 0
					if x > generationWidth {
						x, side = generationWidth-x, 1
					}
					lane := min(2, int(-x/.15))
					lanes[side][lane] += length
				}
				if mid.X > .18 && mid.X < .82 {
					middle += length
					reachesMiddle = true
				}
				// Independently check the exact footprint, not the optimized
				// signed edge query used by the planner itself.
				pad := max(a.Radius, b.Radius) + backgroundVineRockClearance
				if footprint.background.distance(a.P) >= -a.Radius-.010 || footprint.background.distance(b.P) >= -b.Radius-.010 {
					t.Fatal("vine left the actual background cells")
				}
				if footprint.distance(a.P) <= pad || footprint.distance(b.P) <= pad {
					t.Fatal("optimized vine routing placed a ribbon inside foreground rock")
				}
			}
			if fromPadding && v.Parent >= 0 && reachesMiddle {
				connected++
			}
			for _, leaf := range v.Leaves {
				for _, p := range leaf.Polygon {
					if footprint.distance(p) <= backgroundVineRockClearance {
						t.Fatal("optimized leaf fitting placed a leaf inside foreground rock")
					}
				}
			}
		}
		t.Logf("%s: outer length %.2f, middle length %.2f, inward branches %d, lane coverage %v", mode, outer, middle, connected, lanes)
		if fraction := outer / tc.previousOuter; fraction < .30 || fraction > .40 {
			t.Fatalf("padding density is %.1f%% of the previous layout, want 30–40%%", fraction*100)
		}
		if middle < 2 || connected < 3 {
			t.Fatal("padding vines did not branch into the middle")
		}
		for _, side := range lanes {
			for _, length := range side {
				if length < 1 {
					t.Fatal("a substantial padding strip still has no vines")
				}
			}
		}
		visibleFraction := func(vines []Vine, check bool) (float64, float64) {
			visible, borders, total := 0.0, 0.0, 0.0
			for i, v := range vines {
				if len(v.Points) < 2 {
					t.Fatal("padding steering produced an empty vine")
				}
				if v.Parent >= 0 && (v.Parent >= i || v.Points[0].P != vines[v.Parent].Points[v.Joint].P) {
					t.Fatal("padding steering detached a fork")
				}
				for j := 1; j < len(v.Points); j++ {
					a, b := v.Points[j-1].P, v.Points[j].P
					length := b.Sub(a).Len()
					d := footprint.distance(geom.LerpVector(a, b, .5))
					if check && !footprint.backgroundFits(a, b, max(v.Points[j-1].Radius, v.Points[j].Radius)) {
						t.Fatalf("%s: background ribbon is hidden by foreground rock", mode)
					}
					total += length
					mid := geom.LerpVector(a, b, .5)
					if mid.X < .18 || mid.X > .82 {
						borders += length
					}
					if d > .008 && d < backgroundRockPadding-.008 {
						visible += length
					}
				}
				if check {
					for _, leaf := range v.Leaves {
						for j, a := range leaf.Polygon {
							if !footprint.backgroundFits(a, leaf.Polygon[(j+1)%len(leaf.Polygon)], .0002) {
								t.Fatal("background leaf is hidden by foreground rock")
							}
						}
					}
				}
			}
			return visible / total, borders / total
		}
		guided, borders := visibleFraction(vines, true)
		t.Logf("%s: formation padding fraction %.3f, border fraction %.3f", mode, guided, borders)
		if guided < .25 || borders < .55 || borders > .95 {
			t.Fatal("background vines must mostly cover borders and also reach the middle")
		}
	}
}

func TestBackgroundVinesWrapPaddingHoles(t *testing.T) {
	// A hole inside an otherwise continuous outer strip, far from foreground.
	rear := newRockFootprint(RockGrid{
		terrainRect(-.5, -.5, .20, 2), terrainRect(-.2, -.5, 1.7, 2),
		terrainRect(-.3, -.5, .1, .8), terrainRect(-.3, .45, .1, 1.05),
	})
	f := newRockFootprint(nil)
	f.background = rear
	root := geom.V{X: -.25, Y: .7}
	if f.paddingSegment(geom.V{X: -.25, Y: .55}, geom.V{X: -.25, Y: .2}, .003) || f.backgroundFits(geom.V{X: -.25, Y: .55}, geom.V{X: -.25, Y: .2}, .003) {
		t.Fatal("padding shortcut bridged a rear hole")
	}
	if f.backgroundFits(geom.V{X: -.25, Y: .38}, geom.V{X: -.25, Y: .38}, .003) {
		t.Fatal("vine root fits in unsupported padding")
	}
	v := freePathInPadding(root, geom.V{Y: -1}, .8, .003, 0, -1, 0, f)
	if len(v.Points) < 20 || v.Points[len(v.Points)-1].P.Y > .25 {
		t.Fatalf("vine stopped instead of wrapping the hole: %v", v.Points)
	}
	for i := 1; i < len(v.Points); i++ {
		a, b := v.Points[i-1], v.Points[i]
		margin := max(a.Radius, b.Radius) + .010
		for _, e := range rear.edges {
			if geom.SegmentDistanceSquared(a.P, b.P, e.A, e.B) <= margin*margin {
				t.Fatal("vine ribbon crossed the hole rim")
			}
		}
	}
}

func TestBackgroundVinesRejectThinPaddingHoles(t *testing.T) {
	// Endpoints and midpoint are supported, but a tiny hole lies between them.
	f := newRockFootprint(nil)
	f.background = newRockFootprint(RockGrid{
		terrainRect(-.5, -.5, .24, 2), terrainRect(-.258, -.5, 1.758, 2),
		terrainRect(-.26, -.5, .002, .7), terrainRect(-.26, .21, .002, 1.29),
	})
	a, b := geom.V{X: -.4, Y: .205}, geom.V{X: -.05, Y: .205}
	for _, p := range []geom.V{a, b, geom.LerpVector(a, b, .5)} {
		if !f.backgroundFits(p, p, .0002) {
			t.Fatal("fixture samples are unsupported")
		}
	}
	if f.backgroundFits(a, b, .0002) {
		t.Fatal("vine bridged a thin padding hole between supported samples")
	}
}

func TestBackgroundVineDistanceMatchesExactBoundaryBand(t *testing.T) {
	f := newRockFootprint(RockGrid{terrainRect(.25, .25, .3, .10), terrainRect(.25, .35, .10, .35), terrainRect(.7, .25, .10, .4)})
	for y := .12; y < .85; y += .013 {
		for x := .12; x < .92; x += .013 {
			p := geom.V{X: x, Y: y}
			want := f.distance(p)
			if math.Abs(want) < backgroundRockPadding+.035 {
				if got := f.vineDistance(p); math.Abs(got-want) > 1e-10 {
					t.Fatalf("capped edge distance differs at %v: %g / %g", p, got, want)
				}
			}
		}
	}
}

func TestBackgroundPaddingFastPathUsesActualForegroundBounds(t *testing.T) {
	f := newRockFootprint(RockGrid{terrainRect(.2, .2, .6, .6)})
	a, b := geom.V{X: -.4, Y: .3}, geom.V{X: -.05, Y: .8}
	if !f.paddingSegment(a, b, .004) || !f.backgroundFits(a, b, .004) {
		t.Fatal("clear padding did not use the analytic path")
	}
	if f.paddingSegment(a, geom.V{X: .4, Y: .4}, .004) || f.backgroundFits(a, geom.V{X: .4, Y: .4}, .004) {
		t.Fatal("padding shortcut allowed an inward branch to pass through rock")
	}
	f = newRockFootprint(RockGrid{terrainRect(-.25, .2, .1, .6)})
	if f.paddingSegment(a, b, .004) || f.backgroundFits(a, b, .004) {
		t.Fatal("padding shortcut ignored a foreground obstacle outside the usual cave bounds")
	}
}

func TestBackgroundVinesRejectRockBetweenClearSamples(t *testing.T) {
	f := newRockFootprint(RockGrid{terrainRect(.04, -.005, .002, .01)})
	a, b := geom.V{X: -.08}, geom.V{X: .12}
	for _, p := range []geom.V{a, b, geom.LerpVector(a, b, .5)} {
		if !f.backgroundFits(p, p, .0002) {
			t.Fatal("fixture samples do not have enough clearance")
		}
	}
	if f.backgroundFits(a, b, .0002) {
		t.Fatal("background ribbon crossed a thin foreground rock between samples")
	}
}

func TestBackgroundVinesIncludeHorizontalRunners(t *testing.T) {
	for _, mode := range []Orientation{Vertical, Horizontal} {
		for _, seed := range []int64{7, 42, 123} {
			for _, id := range []int64{0, 1, 2} {
				vines := backgroundPathVines(seed, id, mode, nil)
				if !reflect.DeepEqual(vines, backgroundPathVines(seed, id, mode, nil)) {
					t.Fatalf("mode %v seed %d section %d: background vines changed on reload", mode, seed, id)
				}
				roots, horizontal := 0, 0
				for i, v := range vines {
					if v.Foreground {
						t.Fatal("background vine became foreground growth")
					}
					lo, hi := v.Points[0].P, v.Points[0].P
					for _, p := range v.Points {
						if p.P.X < .02 || p.P.X > .98 || p.P.Y < GenerationMinY+.02 || p.P.Y > generationMaxY-.02 {
							t.Fatal("background vine escaped the padded section window")
						}
						lo.X, lo.Y = min(lo.X, p.P.X), min(lo.Y, p.P.Y)
						hi.X, hi.Y = max(hi.X, p.P.X), max(hi.Y, p.P.Y)
					}
					if v.Parent >= 0 {
						if v.Parent >= i || v.Points[0].P != vines[v.Parent].Points[v.Joint].P {
							t.Fatal("background fork lost its parent attachment")
						}
						continue
					}
					roots++
					span := WorldPoint(mode, hi.Sub(lo))
					if math.Abs(span.X) > math.Abs(span.Y) {
						horizontal++
						if math.Abs(span.X) < .35 {
							t.Fatal("horizontal background runner is too short")
						}
					}
				}
				wantRoots, wantHorizontal := 7, 2
				if roots != wantRoots || horizontal != wantHorizontal {
					t.Fatalf("mode %v seed %d section %d: got %d roots (%d horizontal), want %d (%d horizontal)", mode, seed, id, roots, horizontal, wantRoots, wantHorizontal)
				}
			}
		}
	}
}

func TestForegroundVineLengths(t *testing.T) {
	for _, mode := range []Orientation{Vertical, Horizontal} {
		roots, length, longest := 0, 0.0, 0.0
		for _, seed := range []int64{7, 42} {
			for _, id := range []int64{0, -1} {
				data := NewSectionBuilder(seed, nil, mode).Build(id)
				for _, v := range data.ForegroundVines {
					if v.Parent >= 0 {
						continue
					}
					if !surfaceVineLongEnough(v) {
						t.Fatalf("mode %v seed %d section %d retained a short isolated vine", mode, seed, id)
					}
					n, _ := v.extent()
					roots++
					length += n
					longest = max(longest, n)
				}
			}
		}
		mean := length / float64(roots)
		t.Logf("mode %v: %d roots, mean length %.4f, longest %.4f", mode, roots, mean, longest)
		if roots < 8 || mean < .20 || longest < .35 {
			t.Fatalf("mode %v: top-cell avoidance shortened the main vines too much", mode)
		}
	}
}

func TestShortVineRetriesFromTheOtherEnd(t *testing.T) {
	g := &vineEdgeGraph{
		points: []geom.V{{X: .2, Y: .2}, {X: .26, Y: .2}, {X: .29, Y: .2}, {X: .4, Y: .6}, {X: .55, Y: .6}, {X: .75, Y: .6}},
		edges:  []vineEdge{{0, 1}, {1, 2}, {3, 4}, {4, 5}},
		links:  [][]int{{0}, {0, 1}, {1}, {2}, {2, 3}, {3}},
	}
	support := newPathSupport(NewRockTopology(RockGrid{terrainRect(.1, .1, .8, .8)}), nil)
	r := newVineRoutesForGraph(g, &support, CellEdgeVineRoute)
	source := Vine{Parent: -1, Points: []VinePoint{{P: g.points[0]}, {P: geom.V{X: .3, Y: .3}}, {P: geom.V{X: .5, Y: .45}}, {P: g.points[5]}}}
	v, _ := r.trunk(source, rand.New(rand.NewSource(42)), 42)
	if len(v.Points) == 0 || !surfaceVineLongEnough(v) || v.Points[0].P != g.points[5] {
		t.Fatal("short root was discarded without trying the longer route from the other end")
	}
	if r.used[0] || r.used[1] || !r.used[2] || !r.used[3] {
		t.Fatal("retry reserved a discarded stub or failed to reserve its longer replacement")
	}
}

func TestShortVineIsOmittedWhenRetriesCannotExtendIt(t *testing.T) {
	g := &vineEdgeGraph{
		points: []geom.V{{X: .2, Y: .2}, {X: .26, Y: .2}, {X: .29, Y: .2}},
		edges:  []vineEdge{{0, 1}, {1, 2}},
		links:  [][]int{{0}, {0, 1}, {1}},
	}
	support := newPathSupport(NewRockTopology(RockGrid{terrainRect(.1, .1, .8, .8)}), nil)
	r := newVineRoutesForGraph(g, &support, CellEdgeVineRoute)
	source := Vine{Parent: -1, Points: []VinePoint{{P: g.points[0]}, {P: g.points[1]}, {P: g.points[2]}}}
	v, joints := r.trunk(source, rand.New(rand.NewSource(42)), 42)
	if len(v.Points) != 0 || len(joints) != 0 || r.used[0] || r.used[1] {
		t.Fatal("an unextendable isolated stub survived or consumed route space")
	}
}

func TestRemovingShortVineFamiliesKeepsAttachedTwigsAndRemapsParents(t *testing.T) {
	short := Vine{Parent: -1, Foreground: true}
	// A tiny curled shape can accumulate plenty of length without useful span.
	for i := 0; i < 10; i++ {
		short.Points = append(short.Points, VinePoint{P: geom.V{X: .6 + float64(i%2)*.03, Y: .6}, Radius: .001})
	}
	long := Vine{Parent: -1, Foreground: true, Points: []VinePoint{{P: geom.V{X: .1, Y: .2}}, {P: geom.V{X: .5, Y: .2}, Radius: .001}, {P: geom.V{X: .6, Y: .2}}}}
	branch := func(parent, joint, depth int, root geom.V) Vine {
		return Vine{Parent: parent, Joint: joint, Depth: depth, Foreground: true, Points: []VinePoint{{P: root, Radius: .0005}, {P: root.Add(geom.V{X: .02, Y: .02})}}}
	}
	source := []Vine{short, long,
		branch(0, 1, 1, short.Points[1].P), branch(1, 1, 1, long.Points[1].P),
		branch(2, 0, 2, short.Points[1].P), branch(3, 0, 2, long.Points[1].P),
		{Parent: -1, Points: []VinePoint{{P: geom.V{X: .8, Y: .8}}, {P: geom.V{X: .82, Y: .82}}}},
	}
	kept := removeShortSurfaceVines(source)
	if len(kept) != 4 || kept[0].Points[0] != long.Points[0] || kept[1].Parent != 0 || kept[2].Parent != 1 || kept[3].Foreground {
		t.Fatal("short-family cleanup damaged surviving branches or background growth")
	}
	for _, v := range kept[:3] {
		if v.Parent >= 0 && v.Points[0].P != kept[v.Parent].Points[v.Joint].P {
			t.Fatal("cleanup detached a surviving twig")
		}
	}
	if source[3].Parent != 1 || source[5].Parent != 3 {
		t.Fatal("cleanup modified the original family attachments")
	}
}

func TestVinesRejectUnallocatedRenderedTopFacingCells(t *testing.T) {
	for _, mode := range []Orientation{Vertical, Horizontal} {
		grid := RockGrid{terrainRect(.1, .2, .8, .1), terrainRect(.1, .3, .8, .4)}
		for i := range grid {
			MapPoints(grid[i].Polygon, mode.internalLocal)
			grid[i].Center = mode.internalLocal(grid[i].Center)
			grid[i].orientation = mode
			grid[i].Normal = geom.V3{Z: 1}
		}
		topology := NewRockTopology(grid)
		topology.Appearance = append(RockGrid(nil), topology.Grid...)
		up := InternalPoint(mode, geom.V{Y: -.6})
		topology.Appearance[0].Normal = geom.V3{X: up.X, Y: up.Y, Z: .8}
		support := newPathSupport(topology, nil)
		for _, route := range []VineRoute{FreeVineRoute, CellEdgeVineRoute, CellCenterVineRoute, GuideVineRoute} {
			s := support.forRoute(route)
			p := mode.internalLocal(geom.V{X: .5, Y: .25})
			if s.fits(p, p, .001) {
				t.Fatalf("mode %v route %v: vine grew on a rendered top-facing cell", mode, route)
			}
			p = mode.internalLocal(geom.V{X: .5, Y: .35})
			if !s.fits(p, p, .001) {
				t.Fatalf("mode %v route %v: vine cannot grow below the top-facing cell", mode, route)
			}
			// An eligible centerline cannot let its outer ribbon cover the top.
			p = mode.internalLocal(geom.V{X: .5, Y: .303})
			if s.fits(p, p, .003) {
				t.Fatal("vine width crossed onto a top-facing cell")
			}
		}
	}
}

func TestSparseVineSegmentsCannotCrossUnallocatedTopFacingCells(t *testing.T) {
	grid := RockGrid{terrainRect(.1, .2, .3, .6), terrainRect(.4, .2, .1, .6), terrainRect(.5, .2, .4, .6)}
	grid[1].Normal = geom.V3{Y: -.6, Z: .8}
	support := newPathSupport(NewRockTopology(grid), nil)
	a, b := geom.V{X: .3, Y: .5}, geom.V{X: .6, Y: .5}
	if !support.fits(a, a, .001) || !support.fits(b, b, .001) {
		t.Fatal("fixture endpoints do not support vines")
	}
	if support.fits(a, b, .001) {
		t.Fatal("sparse ribbon crossed a top-facing cell between its samples")
	}
}
