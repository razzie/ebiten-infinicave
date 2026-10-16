package terrain

import (
	"context"
	"math"
	"math/rand"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestBranchesExtendReliefBeyondGuideBandAndLeaveGaps(t *testing.T) {
	guides := []Guide{SplineGuide([]geom.V{{X: 0.1, Y: 0.4}, {X: 0.9, Y: 0.4}}, 1)}
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	branches := newBranchField(generateBranches(guides, noise, rand.New(rand.NewSource(42))))
	lit, dark := 0, 0
	for y := 0.4 + guideInfluence; y <= 0.4+guideInfluence+0.04; y += 0.01 {
		for x := 0.1; x <= 0.9; x += 0.01 {
			p := geom.V{X: x, Y: y}
			base := reliefHeight(p, guides, noise, nil)
			got := reliefHeight(p, guides, noise, branches)
			if got > base+.006 {
				lit++
			}
			if got < .00001 {
				dark++
			}
		}
	}
	if lit < 10 || dark < 10 {
		t.Errorf("expected distant fingers separated by dark gaps: lit=%d dark=%d", lit, dark)
	}
}

func TestBranchesPreserveGuideShadows(t *testing.T) {
	guides := []Guide{
		SplineGuide([]geom.V{{X: 0.1, Y: 0.4}, {X: 0.9, Y: 0.4}}, 1),
		// A second ridge intercepts some of the first ridge's branches.
		SplineGuide([]geom.V{{X: 0.1, Y: 0.57}, {X: 0.9, Y: 0.57}}, 1),
	}
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	branches := newBranchField(generateBranches(guides, noise, rand.New(rand.NewSource(42))))
	for _, y := range []float64{.392, .375, .350, .562, .545, .520} {
		for x := 0.12; x <= 0.88; x += 0.01 {
			p := geom.V{X: x, Y: y}
			want := reliefHeight(p, guides, noise, nil)
			got := reliefHeight(p, guides, noise, branches)
			if math.Abs(got-want) > 1e-12 {
				t.Fatalf("branch spills onto shadow at %v: got %v, want %v", p, got, want)
			}
		}
	}
}

func TestBranchRegenerationIsDeterministic(t *testing.T) {
	guides := generateGuides(rand.New(rand.NewSource(42)))
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	a := generateBranches(guides, noise, rand.New(rand.NewSource(42)))
	b := generateBranches(guides, noise, rand.New(rand.NewSource(42)))
	c := generateBranches(guides, noise, rand.New(rand.NewSource(43)))
	if len(a) == 0 || !reflect.DeepEqual(a, b) || reflect.DeepEqual(a, c) {
		t.Fatal("branches must reproduce the same seed and vary with a new seed")
	}
}

func TestBranchesStartInSolidRidgesAndKeepConnectedCenterlines(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		for _, seed := range []int64{1, 42, 93} {
			guides := newGuideCache(seed, orientation).window(0)
			noise := NewPerlin(rand.New(rand.NewSource(seed)))
			noise.OffsetY = SectionTop(0)
			segments := generateBranches(guides, noise, rand.New(rand.NewSource(seed)))
			field := newBranchField(segments)
			ends := make(map[geom.V]bool)
			roots := 0
			for _, b := range segments {
				if !ends[b.A] {
					roots++
					if reliefHeight(b.A, guides, noise, nil) <= rockContourHeight {
						t.Fatalf("%v seed %d: spur starts outside solid rock at %v", orientation, seed, b.A)
					}
				}
				for _, u := range []float64{0, .5, 1} {
					p := geom.LerpVector(b.A, b.B, u)
					if reliefHeight(p, guides, noise, field) <= rockContourHeight {
						t.Fatalf("%v seed %d: spur has an invisible centerline at %v", orientation, seed, p)
					}
				}
				ends[b.B] = true
			}
			if roots == 0 {
				t.Fatalf("%v seed %d: all spurs were removed", orientation, seed)
			}
		}
	}
}

func TestBranchesDoNotReappearPastGuideShadow(t *testing.T) {
	// A spur from a full-sized guide used to dip below the contour through
	// another guide's shadow, then reappear past its tip as a detached chip.
	guides := worldGuides(93, 0)
	noise := NewPerlin(rand.New(rand.NewSource(93)))
	noise.OffsetY = SectionTop(0)
	field := newBranchField(generateBranches(guides, noise, rand.New(rand.NewSource(93))))
	for _, p := range []geom.V{{X: .2775872186, Y: .6832382693}, {X: .2776039460, Y: .6833272521}} {
		if height := reliefHeight(p, guides, noise, field); height >= rockContourHeight {
			t.Fatalf("detached branch tip reappeared at %v: height %v", p, height)
		}
	}
}

func TestBranchForkStopsBeforeShadowAndScreenFade(t *testing.T) {
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	start := geom.V{X: .5, Y: .3}
	direction := geom.V{Y: 1}
	guide := SplineGuide([]geom.V{{X: .1, Y: .5}, {X: .9, Y: .5}}, 1)
	open := appendBranchFork(nil, start, direction, .4, .03, .5, 0, noise, nil)
	blocked := appendBranchFork(nil, start, direction, .4, .03, .5, 0, noise, []Guide{guide})
	if len(blocked) == 0 || len(blocked) >= len(open) {
		t.Fatal("fork did not stop when approaching the guide's shadow")
	}
	for _, b := range blocked {
		if b.B.Y >= .4 {
			t.Fatalf("fork continued through the shadow toward the lit side: %v", b.B)
		}
	}
	// The edge fade can erase a fork root even when the curve later bends
	// inward to visible terrain. It must never start in that erased region.
	if fork := appendBranchFork(nil, geom.V{X: .025, Y: .3}, geom.V{X: 1}, .4, .03, .5, 0, noise, nil); len(fork) != 0 {
		t.Fatal("fork grew from an invisible root at the screen edge")
	}
}

func TestBranchForkDoesNotGrowBelowTerrainSamplingWidth(t *testing.T) {
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	start, direction := geom.V{X: .5, Y: .3}, geom.V{Y: 1}
	if fork := appendBranchFork(nil, start, direction, .4, .004, .5, 0, noise, nil); len(fork) != 0 {
		t.Fatal("a spur too narrow for terrain samples was generated")
	}
	if fork := appendBranchFork(nil, start, direction, .4, .03, .5, 0, noise, nil); len(fork) == 0 {
		t.Fatal("a spur with a resolvable solid width was removed")
	}
}

func TestLoopsCreateIrregularRoutesAndSecondaryCycles(t *testing.T) {
	g := SplineGuide([]geom.V{{X: .1, Y: .3}, {X: .9, Y: .3}}, 1)
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	secondary, bent := 0, 0
	for _, seed := range []int64{1, 2, 3, 4, 5, 42} {
		segs := appendLoops(nil, &g, []Guide{g}, noise, rand.New(rand.NewSource(seed)))
		ends := map[geom.V]bool{}
		positive, negative := 0., 0.
		for i, s := range segs {
			if ends[s.B] {
				secondary++
			}
			ends[s.B] = true
			if i > 0 && segs[i-1].B == s.A {
				u, v := segs[i-1].B.Sub(segs[i-1].A).Norm(), s.B.Sub(s.A).Norm()
				turn := math.Atan2(geom.Cross(u, v), u.Dot(v))
				positive += math.Max(0, turn)
				negative += math.Max(0, -turn)
			}
		}
		if positive > .5 && negative > .5 {
			bent++
		}
		t.Logf("seed %d: segments %d, turn +%g/-%g", seed, len(segs), positive, negative)
	}
	if secondary < 2 {
		t.Fatalf("expected reconnecting side loops, got %d", secondary)
	}
	if bent < 4 {
		t.Fatalf("routes still resemble convex arches: only %d irregular networks", bent)
	}
}

func TestCleanRockJunctionsPreservesLoopWallShape(t *testing.T) {
	poly := []geom.V{{X: .6301976352679215, Y: -.7095029094499972}, {X: .659314623305597, Y: -.7171629058550535}, {X: .6734556475925538, Y: -.6908202201826544}, {X: .6725814915714351, Y: -.6891895292662497}, {X: .6620817520839947, Y: -.6821830796557632}, {X: .6551733721967401, Y: -.6831930501293715}, {X: .6542811946690035, Y: -.6827121449679079}, {X: .6537851933758007, Y: -.6824448662393885}, {X: .6535870822295619, Y: -.6823381165317541}, {X: .6535855170155359, Y: -.6823372731357977}, {X: .653479718503579, Y: -.6822802649493381}}
	grid := RockGrid{{Polygon: poly}}
	area := geom.PolygonArea(poly)
	cleanRockJunctions(grid)
	if len(geom.Triangulate(grid[0].Polygon)) != len(grid[0].Polygon)-2 || math.Abs(area-geom.PolygonArea(grid[0].Polygon)) > 1e-13 {
		t.Fatal("loop wall was not repaired without changing its area")
	}
}

func TestBranchThicknessControlsAllStrategies(t *testing.T) {
	guides := []Guide{SplineGuide([]geom.V{{X: .1, Y: .3}, {X: .9, Y: .3}}, 1)}
	guides[0].Seed = 42
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	for _, strategy := range []BranchingStrategy{BranchingSimple, BranchingRecursive, BranchingLoops} {
		t.Run(strategy.String(), func(t *testing.T) {
			generate := func(lo, hi float64) []BranchSegment {
				shape := ShapeOptions{BranchingStrategy: strategy, BranchMinThickness: lo, BranchMaxThickness: hi}
				return generateBranchesWithOptions(guides, noise, rand.New(rand.NewSource(42)), shape)
			}
			for _, thickness := range []float64{.052, .072} {
				segs := generate(thickness, thickness)
				if len(segs) == 0 {
					t.Fatal("no branches")
				}
				if math.Abs(segs[0].WidthA*2-thickness) > 1e-12 {
					t.Fatalf("root thickness %g, want %g", segs[0].WidthA*2, thickness)
				}
				for _, seg := range segs {
					if seg.WidthA*2 > thickness+1e-12 || seg.WidthB*2 > thickness+1e-12 {
						t.Fatalf("thickness exceeds maximum: %+v", seg)
					}
					if strategy == BranchingLoops && (math.Abs(seg.WidthA*2-thickness) > 1e-12 || math.Abs(seg.WidthB*2-thickness) > 1e-12) {
						t.Fatal("fixed loop thickness changed")
					}
				}
			}
			segs := generate(.052, .072)
			if !reflect.DeepEqual(segs, generate(.052, .072)) {
				t.Fatal("thickness is nondeterministic")
			}
			minRoot, maxRoot := math.Inf(1), 0.0
			ends := map[geom.V]bool{}
			for _, seg := range segs {
				// Trunks/forks start at fresh A points or existing junctions. Loop
				// profiles have no tip taper, so all their samples must stay in range.
				if !ends[seg.A] || strategy == BranchingLoops {
					minRoot = math.Min(minRoot, seg.WidthA*2)
					maxRoot = math.Max(maxRoot, seg.WidthA*2)
					if seg.WidthA*2 < .052-1e-12 || seg.WidthA*2 > .072+1e-12 {
						t.Fatalf("nominal thickness out of range: %g", seg.WidthA*2)
					}
				}
				ends[seg.B] = true
			}
			if maxRoot-minRoot < .001 {
				t.Fatalf("no thickness variation: %g to %g", minRoot, maxRoot)
			}
		})
	}
}

func TestBranchNegativePointsSuppressShouldersAndPreserveConnections(t *testing.T) {
	guides := []Guide{SplineGuide([]geom.V{{X: .1, Y: .3}, {X: .9, Y: .3}}, 1)}
	guides[0].Seed = 42
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	for _, strategy := range []BranchingStrategy{BranchingSimple, BranchingRecursive, BranchingLoops} {
		t.Run(strategy.String(), func(t *testing.T) {
			segs := generateBranchesWithStrategy(guides, noise, rand.New(rand.NewSource(42)), strategy)
			plain, shaped := newBranchField(segs), newShapedBranchField(segs, strategy)
			if strategy == BranchingSimple {
				if shaped.negative != nil {
					t.Fatal("simple branches have negative points")
				}
				return
			}
			if len(shaped.negative.segs) == 0 {
				t.Fatal("missing negative points")
			}
			for _, seg := range segs {
				for _, u := range []float64{0, .5, 1} {
					p := geom.LerpVector(seg.A, seg.B, u)
					if reliefHeight(p, guides, noise, shaped) != reliefHeight(p, guides, noise, plain) {
						t.Fatalf("negative point weakened centerline %v", p)
					}
				}
			}
			removed := 0
			for y := .3; y < .9; y += .005 {
				for x := .1; x < .9; x += .005 {
					p := geom.V{X: x, Y: y}
					a, b := reliefHeight(p, guides, noise, plain), reliefHeight(p, guides, noise, shaped)
					if b > a+1e-12 {
						t.Fatal("negative point added rock")
					}
					if a > rockContourHeight && b < rockContourHeight {
						removed++
					}
				}
			}
			if removed < 20 {
				t.Fatalf("negative points did not create open space: %d removed samples", removed)
			}
		})
	}
}

func TestNegativePointInfluenceFallsNearBranches(t *testing.T) {
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	p := geom.V{X: .5, Y: .5}
	field := &BranchField{negative: newBranchField([]BranchSegment{{A: p, B: p, WidthA: .04, WidthB: .04, LightA: .028, LightB: .028}})}
	previous := math.Inf(1)
	for _, positive := range []float64{0, .12, .25, .38, .5} {
		influence := branchNegativeBias(p, field, positive, 0, noise)
		if influence >= previous && positive < .5 {
			t.Fatalf("negative influence does not fade toward a branch: %g >= %g", influence, previous)
		}
		previous = influence
	}
	if previous != 0 {
		t.Fatal("branch core was not protected")
	}
	if branchNegativeBias(p, field, 0, .06, noise) != 0 {
		t.Fatal("guide crest was not protected")
	}
}

func TestNegativePointsKeepWorldCoordinatesAcrossWindows(t *testing.T) {
	guide := SplineGuide([]geom.V{{X: .1, Y: .3}, {X: .9, Y: .3}}, 1)
	guide.Seed = 42
	for _, strategy := range []BranchingStrategy{BranchingRecursive, BranchingLoops} {
		noise := NewPerlin(rand.New(rand.NewSource(42)))
		first := newShapedBranchField(generateBranchesWithStrategy([]Guide{guide}, noise, rand.New(rand.NewSource(42)), strategy), strategy)
		shifted := shiftedGuide(guide, 1)
		noise.OffsetY = -1
		second := newShapedBranchField(generateBranchesWithStrategy([]Guide{shifted}, noise, rand.New(rand.NewSource(42)), strategy), strategy)
		if len(first.negative.segs) != len(second.negative.segs) {
			t.Fatal("point count depends on window")
		}
		for i, point := range first.negative.segs {
			other := second.negative.segs[i]
			if point.A.Sub(other.A.Add(geom.V{Y: -1})).Len() > 1e-9 || math.Abs(point.WidthA-other.WidthA) > 1e-9 {
				t.Fatal("negative points changed across windows")
			}
		}
		for x := .1; x < .9; x += .017 {
			for y := .303; y < .9; y += .019 {
				p := geom.V{X: x, Y: y}
				noise.OffsetY = 0
				a := reliefHeight(p, []Guide{guide}, noise, first)
				noise.OffsetY = -1
				b := reliefHeight(p.Add(geom.V{Y: 1}), []Guide{shifted}, noise, second)
				if math.Abs(a-b) > 1e-9 {
					t.Fatalf("%v: negative relief changed across windows at %v: %g vs %g", strategy, p, a, b)
				}
			}
		}
	}
}

func TestBranchingStrategiesDeterministicAndConnected(t *testing.T) {
	guides := []Guide{SplineGuide([]geom.V{{X: .1, Y: .3}, {X: .9, Y: .3}}, 1)}
	guides[0].Seed = 42
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	for _, strategy := range []BranchingStrategy{BranchingSimple, BranchingRecursive, BranchingLoops} {
		t.Run(strategy.String(), func(t *testing.T) {
			segs := generateBranchesWithStrategy(guides, noise, rand.New(rand.NewSource(42)), strategy)
			if len(segs) == 0 || !reflect.DeepEqual(segs, generateBranchesWithStrategy(guides, noise, rand.New(rand.NewSource(42)), strategy)) {
				t.Fatal("empty or nondeterministic branches")
			}
			field := newShapedBranchField(segs, strategy)
			ends := map[geom.V]bool{}
			for _, seg := range segs {
				if !ends[seg.A] && reliefHeight(seg.A, guides, noise, nil) <= rockContourHeight {
					t.Fatalf("detached root %v", seg.A)
				}
				for _, u := range []float64{0, .5, 1} {
					p := geom.LerpVector(seg.A, seg.B, u)
					if reliefHeight(p, guides, noise, field) <= rockContourHeight {
						t.Fatalf("invisible centerline %v", p)
					}
				}
				ends[seg.B] = true
			}
		})
	}
	simple := generateBranches(guides, noise, rand.New(rand.NewSource(42)))
	if !reflect.DeepEqual(simple, generateBranchesWithStrategy(guides, noise, rand.New(rand.NewSource(42)), BranchingSimple)) {
		t.Fatal("simple strategy changed")
	}
	recursive := generateBranchesWithStrategy(guides, noise, rand.New(rand.NewSource(42)), BranchingRecursive)
	furthest := func(segs []BranchSegment) float64 {
		d := 0.
		for _, s := range segs {
			d = math.Max(d, s.B.Y-.3)
		}
		return d
	}
	if furthest(recursive) < furthest(simple)+.08 {
		t.Fatalf("recursive limbs are not longer: simple %g recursive %g", furthest(simple), furthest(recursive))
	}
	// Junction ancestry counts successive generations, rather than merely
	// counting many forks attached independently to the original guide.
	generations := map[geom.V]int{}
	children := map[geom.V]int{}
	maxGeneration := 0
	for _, s := range recursive {
		children[s.A]++
	}
	for _, s := range recursive {
		generation := generations[s.A]
		if children[s.A] > 1 {
			generation++
		}
		generations[s.B] = generation
		maxGeneration = max(maxGeneration, generation)
	}
	if maxGeneration < 2 {
		t.Fatalf("no secondary branching: %d generations", maxGeneration)
	}
}

func TestLoopsProducesCollisionHoles(t *testing.T) {
	loader := func(id int64) SectionContent {
		if id != 0 {
			return SectionContent{}
		}
		return SectionContent{Guides: []Guide{SplineGuide([]geom.V{{X: .1, Y: .3}, {X: .9, Y: .3}}, 1)}}
	}
	builder := NewSectionBuilder(42, loader)
	builder.Shape = DefaultShapeOptions()
	builder.Shape.BranchingStrategy = BranchingLoops
	build := builder.Begin(context.Background(), 0)
	geometry := PrepareTerrainGeometry(build.Data(), 0).Collision
	// Collision can represent a degenerate union as disjoint faces. Flood
	// empty space using Contains so the check covers that representation too.
	const side = 200
	empty, visited := make([]bool, side*side), make([]bool, side*side)
	var queue []int
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			i := y*side + x
			empty[i] = !geometry.Contains(geom.V{X: (float64(x) + .5) / side, Y: geometry.Top + (float64(y)+.5)/side})
			if empty[i] && (x == 0 || y == 0 || x == side-1 || y == side-1) {
				queue = append(queue, i)
				visited[i] = true
			}
		}
	}
	for at := 0; at < len(queue); at++ {
		i := queue[at]
		for _, next := range []int{i - 1, i + 1, i - side, i + side} {
			if next >= 0 && next < len(empty) && empty[next] && !visited[next] {
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}
	interior := 0
	for i := range empty {
		if empty[i] && !visited[i] {
			interior++
		}
	}
	if interior < 100 {
		t.Fatalf("loops did not leave a substantial enclosed collision opening: %d empty samples", interior)
	}
}

func TestLoopsBridgesStopAtGuideShadows(t *testing.T) {
	g := SplineGuide([]geom.V{{X: .1, Y: .3}, {X: .9, Y: .3}}, 1)
	blocker := SplineGuide([]geom.V{{X: .1, Y: .45}, {X: .9, Y: .45}}, 1)
	noise := NewPerlin(rand.New(rand.NewSource(42)))
	open := appendLoops(nil, &g, []Guide{g}, noise, rand.New(rand.NewSource(42)))
	blocked := appendLoops(nil, &g, []Guide{g, blocker}, noise, rand.New(rand.NewSource(42)))
	if len(open) == 0 || len(blocked) != 0 {
		t.Fatalf("bridges should commit only complete unmasked routes: open=%d blocked=%d", len(open), len(blocked))
	}
}

func TestBranchStrategiesKeepWorldCoordinatesAcrossWindows(t *testing.T) {
	original := SplineGuide([]geom.V{{X: .1, Y: .3}, {X: .9, Y: .3}}, 1)
	original.Seed = 42
	for _, strategy := range []BranchingStrategy{BranchingSimple, BranchingRecursive, BranchingLoops} {
		noise := NewPerlin(rand.New(rand.NewSource(42)))
		a := generateBranchesWithStrategy([]Guide{original}, noise, rand.New(rand.NewSource(42)), strategy)
		shifted := shiftedGuide(original, 1)
		noise.OffsetY = -1
		b := generateBranchesWithStrategy([]Guide{shifted}, noise, rand.New(rand.NewSource(42)), strategy)
		if len(a) != len(b) {
			t.Fatalf("%v: segment count depends on window", strategy)
		}
		for i, s := range a {
			if s.A.Sub(b[i].A.Add(geom.V{Y: -1})).Len() > 1e-9 || s.B.Sub(b[i].B.Add(geom.V{Y: -1})).Len() > 1e-9 || math.Abs(s.WidthA-b[i].WidthA) > 1e-9 {
				t.Fatalf("%v: branch changed between windows", strategy)
			}
		}
	}
}

func TestAllBranchStrategiesProduceValidRoughRockFaces(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		for _, strategy := range []BranchingStrategy{BranchingSimple, BranchingRecursive, BranchingLoops} {
			builder := NewSectionBuilder(42, nil, orientation)
			builder.ForceGuidesUp = true
			builder.Shape = ShapeOptions{BranchingStrategy: strategy, SpikeDensity: 1, EdgeRoughness: 1}
			build := builder.Begin(context.Background(), 0)
			if len(build.Data().Foreground) == 0 {
				t.Fatalf("%v/%v generated empty terrain", orientation, strategy)
			}
			for _, c := range build.Data().Foreground {
				if geom.PolygonArea(c.Polygon) <= 0 || !simpleRockOutline(c.Polygon) || len(geom.Triangulate(c.Polygon)) != len(c.Polygon)-2 {
					t.Fatalf("%v/%v generated an invalid chipped face: area=%g triangles=%d points=%v", orientation, strategy, geom.PolygonArea(c.Polygon), len(geom.Triangulate(c.Polygon)), c.Polygon)
				}
				for _, p := range c.Polygon {
					if p.X < 0 || p.X > 1 || p.Y < GenerationMinY || p.Y > generationMaxY {
						t.Fatalf("%v/%v grew outside the window: %v", orientation, strategy, p)
					}
				}
			}
		}
	}
}
