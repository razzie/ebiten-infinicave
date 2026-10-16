package terrain

import (
	"image/color"
	"math"
	"math/rand"
	"reflect"
	"runtime"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestChamferFieldMatchesSingleObstacleDistances(t *testing.T) {
	d := takeVineField()
	for i := range d {
		d[i] = math.Inf(1)
	}
	const cx, cy = 173, 611
	d[cy*vineFieldWidth+cx] = 0
	chamferVineField(d)
	for y := 0; y < vineFieldHeight; y++ {
		for x := 0; x < vineFieldWidth; x++ {
			dx, dy := abs(x-cx), abs(y-cy)
			want := float64(max(dx, dy)-min(dx, dy))*vineFieldStep + float64(min(dx, dy))*vineFieldStep*math.Sqrt2
			if math.Abs(d[y*vineFieldWidth+x]-want) > 1e-10 {
				t.Fatalf("distance at (%d,%d) = %g, want %g", x, y, d[y*vineFieldWidth+x], want)
			}
		}
	}
	putVineField(d)
	d = takeVineField()
	defer putVineField(d)
	for _, value := range d {
		if value != 0 {
			t.Fatal("pooled distance buffer contains old terrain")
		}
	}
}

func TestVineWorkspaceSurvivesGCWithoutLeakingOldSamples(t *testing.T) {
	var workspace vineWorkspace
	field := workspace.take()
	field[0], field[len(field)-1] = 123, 456
	address := &field[0]
	workspace.put(field)
	runtime.GC()
	reused := workspace.take()
	if &reused[0] != address || reused[0] != 0 || reused[len(reused)-1] != 0 {
		t.Fatal("worker workspace lost its buffer or retained the previous section")
	}
}

func TestForegroundVinesStayOnVisibleRock(t *testing.T) {
	// Bright and shadowed rock share a continuous surface, with a real gap
	// to the right. The growth field must use geometry instead of shading.
	rock := RockGrid{
		{Polygon: []geom.V{{X: 0, Y: GenerationMinY}, {X: 0.4, Y: GenerationMinY}, {X: 0.4, Y: generationMaxY}, {X: 0, Y: generationMaxY}}, Color: color.NRGBA{220, 210, 190, 255}},
		{Polygon: []geom.V{{X: 0.4, Y: GenerationMinY}, {X: 0.7, Y: GenerationMinY}, {X: 0.7, Y: generationMaxY}, {X: 0.4, Y: generationMaxY}}, Color: color.NRGBA{8, 8, 8, 255}},
		{Polygon: []geom.V{{X: 0.8, Y: GenerationMinY}, {X: generationWidth, Y: GenerationMinY}, {X: generationWidth, Y: generationMaxY}, {X: 0.8, Y: generationMaxY}}},
	}
	field := newForegroundVineTerrain(rock, nil)
	for _, p := range []geom.V{{X: 0.2, Y: 1.5}, {X: 0.5, Y: 1.5}, {X: 0.4, Y: 1.5}} {
		if field.growthSpace(p) < .015 {
			t.Fatalf("surface shading blocked growth at %v", p)
		}
	}
	for _, p := range []geom.V{{X: 0.75, Y: 1.5}, {X: 0.9, Y: 1.5}, {X: 0.005, Y: 1.5}, {X: 0.7, Y: 1.5}} {
		if field.growthSpace(p) > 0 {
			t.Fatalf("surface vines can grow off visible rock at %v", p)
		}
	}
	if rock[0].Color.R != 220 || rock[0].Polygon[0].X != 0 {
		t.Fatal("building growth support modified the rendered rock")
	}
	generate := func(seed int64) []Vine {
		return generateForegroundVines(rock, nil, rand.New(rand.NewSource(seed)))
	}
	vines := generate(42)
	if len(vines) < 2 || !reflect.DeepEqual(vines, generate(42)) || reflect.DeepEqual(vines, generate(43)) {
		t.Fatal("foreground vines need branching, reproducible, seed-dependent growth")
	}
	trunks := 0
	for i, v := range vines {
		if v.Parent < 0 {
			trunks++
		}
		if !v.Foreground || (v.Points[0].Radius != 0 && v.Parent < 0) || v.Points[len(v.Points)-1].Radius != 0 {
			t.Fatal("foreground vine lost its material or tapered tips")
		}
		for _, p := range v.Points {
			if p.Radius > foregroundVineMaxRadius {
				t.Fatal("foreground vine exceeds its reduced thickness cap")
			}
			if field.growthSpace(p.P)+1e-6 < p.Radius {
				t.Fatalf("vine ribbon exceeds its supporting rock: %+v", p)
			}
		}
		if v.Parent >= 0 {
			if v.Parent >= i || v.Points[0].P != vines[v.Parent].Points[v.Joint].P {
				t.Fatal("foreground branch detached from its parent")
			}
		}
	}
	if trunks > 6 {
		t.Fatalf("foreground vines should remain sparse, got %d trunks", trunks)
	}
	if got := generateForegroundVines(nil, nil, rand.New(rand.NewSource(42))); len(got) != 0 {
		t.Fatal("foreground vines grew without supporting rock")
	}
}

func TestForegroundVinesKeepClearOfGuideLines(t *testing.T) {
	rock := testRockGrid([]geom.V{{X: 0.5, Y: 1.5}}, []color.NRGBA{{100, 90, 80, 255}})
	guides := []Guide{SplineGuide([]geom.V{{X: 0.35, Y: GenerationMinY}, {X: 0.42, Y: 0}, {X: 0.3, Y: 1}, {X: 0.38, Y: generationMaxY}}, 1)}
	field := newForegroundVineTerrain(rock, guides)
	guide := &guides[0]
	for _, s := range []float64{0, .750, 1.500, 2.250, guide.S[len(guide.S)-1]} {
		p, _, normal := guide.frameAt(s)
		for _, side := range []float64{-1, 1} {
			if field.growthSpace(p.Add(normal.Mul(side*(foregroundVineGuideClearance-.002)))) != 0 {
				t.Fatal("foreground vines can approach a guide line on one side")
			}
		}
	}
	vines := generateForegroundVines(rock, guides, rand.New(rand.NewSource(42)))
	if len(vines) == 0 {
		t.Fatal("guide avoidance blocked the entire supporting rock")
	}
	for _, v := range vines {
		for j, p := range v.Points {
			if guide.project(p.P).Dist-p.Radius < foregroundVineGuideClearance {
				t.Fatalf("foreground vine ribbon approaches a guide: %+v", p)
			}
			if j > 0 {
				prev := v.Points[j-1]
				mid := geom.LerpVector(prev.P, p.P, .5)
				if guide.project(mid).Dist-math.Max(prev.Radius, p.Radius) < foregroundVineGuideClearance {
					t.Fatal("vine segment crosses the guide exclusion band")
				}
			}
		}
	}
}

func TestForegroundVinesSpreadAcrossSeparateRocks(t *testing.T) {
	// Six equally viable islands should each receive a small family rather
	// than concentrating all branches on the first one or two large trunks.
	var rock RockGrid
	for row := range 3 {
		for column := range 2 {
			x, y := .12+float64(column)*.5, .04+float64(row)*.32
			poly := []geom.V{{X: x, Y: y}, {X: x + .24, Y: y}, {X: x + .24, Y: y + .25}, {X: x, Y: y + .25}}
			rock = append(rock, RockCell{Polygon: poly, Center: geom.PolygonCenter(poly), Color: color.NRGBA{100, 90, 80, 255}})
		}
	}
	for _, seed := range []int64{42, 123, 789} {
		vines := generateForegroundVines(rock, nil, rand.New(rand.NewSource(seed)))
		covered := make(map[int]bool)
		for _, vine := range vines {
			if vine.Parent >= 0 {
				continue
			}
			for i, cell := range rock {
				if geom.InsidePolygon(vine.Points[len(vine.Points)/2].P, cell.Polygon) {
					covered[i] = true
				}
			}
		}
		if len(covered) != len(rock) {
			t.Fatalf("seed %d populated %d of %d viable rocks", seed, len(covered), len(rock))
		}
	}
}

func TestForegroundGrowthAvoidsExistingStems(t *testing.T) {
	rock := testRockGrid([]geom.V{{X: .5, Y: .5}}, []color.NRGBA{{100, 90, 80, 255}})
	field := newForegroundVineTerrain(rock, nil)
	defer field.release()
	field.occupied = make(vineTwigOccupancy)
	stem := straightStyleVine(.5, .8)
	for i := range stem.Points {
		stem.Points[i].Radius = .0015
	}
	field.occupied.add(stem)
	vine := growVine(field, geom.V{X: .4, Y: .5}, geom.V{X: 1}, .0012, .3, 0, 0, 0)
	if length, _ := vine.extent(); length < .06 {
		t.Fatal("occupancy blocked growth before approaching the existing stem")
	}
	for _, p := range vine.Points {
		if p.P.X+p.Radius >= .5-.0015 {
			t.Fatal("new foreground trunk overlaps an existing ribbon")
		}
	}
}

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
