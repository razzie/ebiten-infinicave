package infinicave

import (
	"math"
	"reflect"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestHorizontalViewportAndPrefetch(t *testing.T) {
	for _, input := range []Viewport{
		{}, {Width: -1}, {X: -.001, Width: 1}, {X: math.NaN(), Width: 1},
		{X: math.Inf(1), Width: 1}, {Width: math.Inf(1)}, {Width: 1, Velocity: math.NaN()},
		{X: math.MaxFloat64, Width: math.MaxFloat64},
	} {
		if _, valid := orientedViewport(Horizontal, input); valid {
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
		v, valid := orientedViewport(Horizontal, tc.viewport)
		low, high := visibleSections(v.Y, v.Height)
		if !valid || low != tc.low || high != tc.high {
			t.Fatalf("horizontal sections for %+v: %d..%d", tc.viewport, low, high)
		}
	}
	forward, _ := orientedViewport(Horizontal, Viewport{X: 10, Width: 1, Velocity: .02})
	backward, _ := orientedViewport(Horizontal, Viewport{X: 10, Width: 1, Velocity: -.02})
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
	scene := queryScene(terrain.SectionData{Foreground: RockGrid{terrainRect(.2, .2, .6, .6)},
		Guides: []Guide{terrain.SplineGuide([]V{{X: .3, Y: .4}, {X: .3, Y: .7}}, 1)},
	})
	scene.orientation = Horizontal
	scene.world.done = make(chan struct{})
	t.Cleanup(scene.Close)
	ray := Ray{Origin: V{X: .1, Y: .5}, Direction: V{X: 1, Y: 0}, MaxDistance: .8}
	hit := mustQuery(t, scene, ray, QueryOptions{Targets: TargetRock})
	if !hit.Found || !hit.Complete || hit.Hit.Point.Sub(V{X: .2, Y: .5}).Len() > 1e-9 ||
		hit.Hit.Normal != (V{X: -1, Y: 0}) || math.Abs(hit.Hit.Distance-.1) > 1e-9 {
		t.Fatalf("horizontal ray result: %+v", hit)
	}
	formation, ok := scene.Formation(hit.Hit.FormationID)
	if !ok || !formation.Contains(V{X: .5, Y: .5}) || formation.Min.Sub(V{X: .2, Y: .2}).Len() > 1e-9 || formation.Max.Sub(V{X: .8, Y: .8}).Len() > 1e-9 {
		t.Fatalf("horizontal formation: %+v", formation)
	}
	guideHit := mustQuery(t, scene, Ray{Origin: V{X: .5, Y: .3}}, QueryOptions{Targets: TargetGuide})
	guide, ok := scene.Guide(guideHit.Hit.GuideID)
	if !ok || guide.Points[0].Sub(V{X: .6, Y: .3}).Len() > 1e-9 || guide.Points[len(guide.Points)-1].Sub(V{X: .3, Y: .3}).Len() > 1e-9 {
		t.Fatalf("horizontal guide: %+v", guide)
	}
	collision, ok := scene.CollisionGeometry(0)
	if !ok || collision.Origin != (V{}) || collision.Min != (V{}) || collision.Max != (V{X: 1, Y: 1}) || !collision.Contains(V{X: .5, Y: .5}) {
		t.Fatalf("horizontal collision: %+v", collision)
	}
	for _, origin := range []V{{X: -.1, Y: .5}, {X: .5, Y: -.1}, {X: .5, Y: 1.1}} {
		if result := mustQuery(t, scene, Ray{Origin: origin}, QueryOptions{}); result.Found || !result.Complete {
			t.Fatalf("outside horizontal strip: %+v", result)
		}
	}
	result, err := scene.CarveSegment(V{X: .45, Y: .1}, V{X: .45, Y: .9}, .08)
	if err != nil || len(result.Changes) != 1 || len(result.Changes[0].Remaining) != 2 || !reflect.DeepEqual(result.SectionIDs, []int64{0}) {
		t.Fatalf("horizontal split: %+v / %v", result, err)
	}
	collision, _ = scene.CollisionGeometry(0)
	if collision.Contains(V{X: .45, Y: .5}) || mustQuery(t, scene, Ray{Origin: V{X: .45, Y: .5}}, QueryOptions{Targets: TargetRock}).Found {
		t.Fatal("horizontal carve did not update query and collision geometry")
	}
	if _, err := scene.CarveCircle(V{X: .65, Y: .5}, .06); err != nil {
		t.Fatal(err)
	}
	// Rebuild an evicted section, applying the stored cuts before publication.
	mesh := resolutionTestMesh(0)
	scene.applyStoredCuts(&mesh)
	scene.world.sections[0].geometry = mesh.Geometry
	scene.world.revision++
	if mustQuery(t, scene, Ray{Origin: V{X: .65, Y: .5}}, QueryOptions{Targets: TargetRock}).Found {
		t.Fatal("horizontal cut disappeared after reload")
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
	v, _ := orientedViewport(Horizontal, viewport)
	scene.bats.Step(v, 6)
	if len(scene.bats.Bats) == 0 {
		t.Fatal("no bat arrived")
	}
	for _, b := range scene.bats.Bats {
		start, end := terrain.WorldPoint(Horizontal, b.Start), terrain.WorldPoint(Horizontal, b.End)
		if math.Abs(end.Y-start.Y) <= math.Abs(end.X-start.X) || start.X < viewport.X || start.X > viewport.X+viewport.Width {
			t.Fatal("landscape bat does not cross vertically through the viewport")
		}
		// After the compositor's quarter turn, banking stays near upright.
		if worldAngle := scene.bats.SilhouetteAngle(b, .5) + math.Pi/2; math.Abs(worldAngle) > .35 {
			t.Fatal("landscape bat silhouette is sideways")
		}
		previous := start
		for i := 1; i <= 100; i++ {
			point := terrain.WorldPoint(Horizontal, b.Position(float64(i)/100))
			if (point.Y-previous.Y)*(end.Y-start.Y) <= 0 {
				t.Fatal("landscape bat reverses its vertical path")
			}
			previous = point
		}
	}
	scene.Update(viewport)
	image := ebiten.NewImage(180, 100)
	defer image.Deallocate()
	before := scene.bats.Next
	scene.Draw(image, viewport)
	scene.Draw(image, viewport)
	if scene.bats.Next != before || scene.orientedTarget == nil || scene.orientedTarget.Bounds().Dx() != 100 || scene.orientedTarget.Bounds().Dy() != 180 {
		t.Fatal("horizontal draw advanced animation or used the wrong compositor dimensions")
	}
	scene.Reset(99)
	if scene.Orientation() != Horizontal || scene.fog.Orientation != Horizontal || scene.background.Offset != terrain.InternalPoint(Horizontal, config.ShadowOffset) || scene.bats.Orientation != Horizontal || len(scene.bats.Bats) != 0 {
		t.Fatal("reset lost orientation or effect settings")
	}
	scene.Close()
	if scene.orientedTarget != nil || scene.Update(viewport) {
		t.Fatal("closed horizontal scene retained resources")
	}
}
