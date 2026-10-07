package infinicave

import (
	"math"
	"reflect"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
)

func TestHorizontalViewportAndPrefetch(t *testing.T) {
	for _, input := range []Viewport{
		{}, {Width: -1}, {X: -.001, Width: 1}, {X: math.NaN(), Width: 1},
		{X: math.Inf(1), Width: 1}, {Width: math.Inf(1)}, {Width: 1, Velocity: math.NaN()},
		{X: math.MaxFloat64, Width: math.MaxFloat64},
	} {
		if _, valid := Horizontal.viewport(input); valid {
			t.Fatalf("accepted invalid horizontal viewport: %+v", input)
		}
	}
	for _, tc := range []struct {
		viewport  Viewport
		low, high int64
	}{
		{Viewport{Width: 1}, 0, 0},
		{Viewport{X: .001, Width: 1}, 0, 1},
		{Viewport{X: 1, Width: 1}, 1, 1},
		{Viewport{X: 1000000, Width: 1.8}, 1000000, 1000001},
	} {
		v, valid := Horizontal.viewport(tc.viewport)
		low, high := visibleSections(v.Y, v.Height)
		if !valid || low != tc.low || high != tc.high {
			t.Fatalf("horizontal sections for %+v: %d..%d", tc.viewport, low, high)
		}
	}
	forward, _ := Horizontal.viewport(Viewport{X: 10, Width: 1, Velocity: .02})
	backward, _ := Horizontal.viewport(Viewport{X: 10, Width: 1, Velocity: -.02})
	ahead, required := prefetch(forward.Y, forward.Height, forward.Velocity)
	behind, _ := prefetch(backward.Y, backward.Height, backward.Velocity)
	if ahead[required] <= 10 || behind[required] >= 10 {
		t.Fatalf("wrong prefetch directions: %v / %v", ahead, behind)
	}
	if _, err := NewScene(Config{Orientation: Orientation(99)}); err == nil {
		t.Fatal("invalid orientation accepted")
	}
}

func TestHorizontalWorldGeometryAndCarving(t *testing.T) {
	scene := queryScene(sectionData{foreground: RockGrid{terrainRect(.2, .2, .6, .6)},
		guides: []Guide{splineGuide([]V{{.3, .4}, {.3, .7}}, 1)},
	})
	scene.orientation = Horizontal
	scene.world.done = make(chan struct{})
	t.Cleanup(scene.Close)
	ray := Ray{Origin: V{.1, .5}, Direction: V{1, 0}, MaxDistance: .8}
	hit := mustQuery(t, scene, ray, QueryOptions{Targets: TargetRock})
	if !hit.Found || !hit.Complete || hit.Hit.Point.Sub(V{.2, .5}).Len() > 1e-9 ||
		hit.Hit.Normal != (V{-1, 0}) || math.Abs(hit.Hit.Distance-.1) > 1e-9 {
		t.Fatalf("horizontal ray result: %+v", hit)
	}
	formation, ok := scene.Formation(hit.Hit.FormationID)
	if !ok || !formation.Contains(V{.5, .5}) || formation.Min.Sub(V{.2, .2}).Len() > 1e-9 || formation.Max.Sub(V{.8, .8}).Len() > 1e-9 {
		t.Fatalf("horizontal formation: %+v", formation)
	}
	guideHit := mustQuery(t, scene, Ray{Origin: V{.5, .3}}, QueryOptions{Targets: TargetGuide})
	guide, ok := scene.Guide(guideHit.Hit.GuideID)
	if !ok || guide.Points[0].Sub(V{.6, .3}).Len() > 1e-9 || guide.Points[len(guide.Points)-1].Sub(V{.3, .3}).Len() > 1e-9 {
		t.Fatalf("horizontal guide: %+v", guide)
	}
	collision, ok := scene.CollisionGeometry(0)
	if !ok || collision.Origin != (V{}) || collision.Min != (V{}) || collision.Max != (V{1, 1}) || !collision.Contains(V{.5, .5}) {
		t.Fatalf("horizontal collision: %+v", collision)
	}
	for _, origin := range []V{{-.1, .5}, {.5, -.1}, {.5, 1.1}} {
		if result := mustQuery(t, scene, Ray{Origin: origin}, QueryOptions{}); result.Found || !result.Complete {
			t.Fatalf("outside horizontal strip: %+v", result)
		}
	}
	result, err := scene.CarveSegment(V{.45, .1}, V{.45, .9}, .08)
	if err != nil || len(result.Changes) != 1 || len(result.Changes[0].Remaining) != 2 || !reflect.DeepEqual(result.SectionIDs, []int64{0}) {
		t.Fatalf("horizontal split: %+v / %v", result, err)
	}
	collision, _ = scene.CollisionGeometry(0)
	if collision.Contains(V{.45, .5}) || mustQuery(t, scene, Ray{Origin: V{.45, .5}}, QueryOptions{Targets: TargetRock}).Found {
		t.Fatal("horizontal carve did not update query and collision geometry")
	}
	if _, err := scene.CarveCircle(V{.65, .5}, .06); err != nil {
		t.Fatal(err)
	}
	// Rebuild an evicted section, applying the stored cuts before publication.
	mesh := resolutionTestMesh(0)
	scene.applyStoredCuts(&mesh)
	scene.world.sections[0].geometry = mesh.geometry
	scene.world.revision++
	if mustQuery(t, scene, Ray{Origin: V{.65, .5}}, QueryOptions{Targets: TargetRock}).Found {
		t.Fatal("horizontal cut disappeared after reload")
	}
}

func TestHorizontalGeneratedSectionAndAuthoredContent(t *testing.T) {
	points := []V{{.2, .45}, {.75, .45}}
	load := func(id int64) SectionContent {
		if id != -1 {
			return SectionContent{}
		}
		return SectionContent{Guides: []Guide{{Pts: points}}, Holes: []Hole{{Shape: HoleCircle, Center: V{.5, .45}, Radius: .04}}}
	}
	var early CollisionGeometry
	config := Config{Seed: 42, Orientation: Horizontal, LoadSection: load, OnCollisionReady: func(g CollisionGeometry) { early = g }}
	section, err := GenerateSectionWithConfig(config, -1)
	if err != nil {
		t.Fatal(err)
	}
	if section.Orientation != Horizontal || section.Origin != (V{1, 0}) || section.WindowOrigin != (V{}) ||
		section.Min != (V{1, 0}) || section.Max != (V{2, 1}) || !reflect.DeepEqual(early, section.Collision) {
		t.Fatalf("horizontal section metadata: %+v / %+v", section.Origin, early)
	}
	if len(section.Guides) != 1 || len(section.Holes) != 1 {
		t.Fatal("authored content missing")
	}
	for i, p := range section.Guides[0].Pts {
		if p.Sub(points[i]).Len() > 1e-12 {
			t.Fatalf("authored local coordinate changed: %v / %v", p, points[i])
		}
	}
	if section.Collision.Contains(V{1.5, .45}) {
		t.Fatal("authored horizontal hole missing")
	}
	for _, c := range section.Foreground {
		for _, p := range c.Polygon {
			if p.Y < -1e-9 || p.Y > 1+1e-9 || p.X < -1-1e-9 || p.X > 2+1e-9 {
				t.Fatalf("horizontal foreground padding is incorrect: %v", p)
			}
		}
	}
	// The public local faces agree with world collision after adding Origin.
	for x := .03; x < .97; x += .07 {
		for y := .03; y < .97; y += .07 {
			p := V{x, y}
			inside := false
			for _, c := range section.Foreground {
				inside = inside || (CollisionGeometry{Polygons: [][]V{c.Polygon}}).Contains(p)
			}
			if inside != section.Collision.Contains(p.Add(section.Origin)) {
				t.Fatalf("horizontal local faces and collision disagree at %v", p)
			}
		}
	}
	again, err := GenerateSectionWithConfig(config, -1)
	if err != nil || !reflect.DeepEqual(section, again) || !reflect.DeepEqual(points, []V{{.2, .45}, {.75, .45}}) {
		t.Fatal("horizontal generation is not reproducible or mutated loader content")
	}
}

func TestGuideLayoutsPreferWorldHorizontal(t *testing.T) {
	for _, mode := range []Orientation{Vertical, Horizontal} {
		horizontal, total := 0, 0
		cache := newGuideCache(42, mode)
		for id := int64(0); id < 12; id++ {
			cache.window(id)
			for _, g := range cache.resolved[id] {
				var dx, dy float64
				for i := 1; i < len(g.Pts); i++ {
					d := mode.world(g.Pts[i].Sub(g.Pts[i-1]))
					dx += math.Abs(d.X)
					dy += math.Abs(d.Y)
				}
				if dx > dy {
					horizontal++
				}
				total++
			}
		}
		if total == 0 || horizontal*2 <= total {
			t.Fatalf("%v guides are not predominantly horizontal: %d/%d", mode, horizontal, total)
		}
		t.Logf("%v horizontal guides: %d/%d", mode, horizontal, total)
	}
}

func TestHorizontalMushroomsAndLighting(t *testing.T) {
	// In this section frame, a vertical guide maps to a horizontal ledge.
	guide := splineGuide([]V{{.5, .1}, {.5, .9}}, -1)
	guide.Seed = 42
	ground := RockGrid{terrainRect(.5, .05, .4, .9)}
	groups := mushroomsForGuides([]Guide{guide}, ground, Horizontal)
	count := 0
	for _, group := range groups {
		for _, m := range group.Mushrooms {
			count++
			root, cap := Horizontal.world(m.Stem[0]), Horizontal.world(m.CapCenter)
			if cap.Y >= root.Y || Horizontal.world(m.RootDirection).Y > 0 || !mushroomWithinForegroundInset(m) {
				t.Fatal("horizontal mushroom does not grow upward inside the bounded strip")
			}
		}
	}
	if count == 0 {
		t.Fatal("horizontal ledge generated no upright mushrooms")
	}
	up, down := Horizontal.internal(V{0, -1}), Horizontal.internal(V{0, 1})
	if surfaceLight(V3{up.X, up.Y, 1}, Horizontal) <= surfaceLight(V3{down.X, down.Y, 1}, Horizontal) {
		t.Fatal("landscape lighting does not illuminate world-up faces")
	}
	// The normal diagnostic must report world axes too.
	cell := RockCell{Normal: V3{up.X, up.Y, 0}, orientation: Horizontal}
	if clr := rockViewColor(cell, ViewNormals); clr.G != 0 || clr.R != 127 {
		t.Fatalf("horizontal normal diagnostic: %+v", clr)
	}
}

func TestHorizontalBatPathsAndSceneLifecycle(t *testing.T) {
	config := DefaultConfig()
	config.Orientation = Horizontal
	config.Seed = 42
	scene, err := NewScene(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(scene.Close)
	viewport := Viewport{X: 3, Width: 1.8, Velocity: .01}
	v, _ := Horizontal.viewport(viewport)
	scene.bats.step(v, 6)
	if len(scene.bats.bats) == 0 {
		t.Fatal("no bat arrived")
	}
	for _, b := range scene.bats.bats {
		start, end := Horizontal.world(b.start), Horizontal.world(b.end)
		if math.Abs(end.Y-start.Y) <= math.Abs(end.X-start.X) || start.X < viewport.X || start.X > viewport.X+viewport.Width {
			t.Fatal("landscape bat does not cross vertically through the viewport")
		}
		// After the compositor's quarter turn, banking stays near upright.
		if worldAngle := scene.bats.silhouetteAngle(b, .5) + math.Pi/2; math.Abs(worldAngle) > .35 {
			t.Fatal("landscape bat silhouette is sideways")
		}
		previous := start
		for i := 1; i <= 100; i++ {
			point := Horizontal.world(b.position(float64(i) / 100))
			if (point.Y-previous.Y)*(end.Y-start.Y) <= 0 {
				t.Fatal("landscape bat reverses its vertical path")
			}
			previous = point
		}
	}
	scene.Update(viewport)
	image := ebiten.NewImage(180, 100)
	defer image.Deallocate()
	before := scene.bats.next
	scene.Draw(image, viewport)
	scene.Draw(image, viewport)
	if scene.bats.next != before || scene.orientedTarget == nil || scene.orientedTarget.Bounds().Dx() != 100 || scene.orientedTarget.Bounds().Dy() != 180 {
		t.Fatal("horizontal draw advanced animation or used the wrong compositor dimensions")
	}
	scene.Reset(99)
	if scene.Orientation() != Horizontal || scene.fog.orientation != Horizontal || scene.background.offset != Horizontal.internal(config.ShadowOffset) || scene.bats.orientation != Horizontal || len(scene.bats.bats) != 0 {
		t.Fatal("reset lost orientation or effect settings")
	}
	scene.Close()
	if scene.orientedTarget != nil || scene.Update(viewport) {
		t.Fatal("closed horizontal scene retained resources")
	}
}

func TestHorizontalRandomSectionBorderCutoffs(t *testing.T) {
	data := newSectionBuilder(93, nil, Horizontal).build(0)
	grid := insetForegroundGrid(data.foreground)
	if len(grid) == 0 || len(data.vines) == 0 || len(data.foregroundVines) == 0 {
		t.Fatal("horizontal generation omitted rock or vines")
	}
	for _, c := range grid {
		for _, p := range c.Polygon {
			world := Horizontal.local(p)
			if world.Y < foregroundScreenInset-1e-9 || world.Y > 1-foregroundScreenInset+1e-9 {
				t.Fatalf("horizontal rock exceeds top/bottom cutoff: %v", world)
			}
		}
	}
	for _, vines := range [][]Vine{data.vines, data.foregroundVines} {
		for i, v := range vines {
			for _, point := range v.Points {
				p := Horizontal.local(point.P)
				if p.Y < 0 || p.Y > 1 {
					t.Fatalf("horizontal vine escapes top/bottom bounds: %v", p)
				}
			}
			if v.Parent >= 0 && (v.Parent >= i || v.Points[0].P != vines[v.Parent].Points[v.Joint].P) {
				t.Fatal("horizontal vine lost its branch attachment")
			}
		}
	}
}
