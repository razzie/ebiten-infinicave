package infinicave

import (
	"context"
	"image"
	"image/color"
	"math"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestAntialiasingConfiguration(t *testing.T) {
	if DefaultConfig().Antialiasing != AntialiasingFull || (Config{}).Antialiasing != AntialiasingFull {
		t.Fatal("default geometry antialiasing changed")
	}
	for _, mode := range []AntialiasingMode{AntialiasingFull, AntialiasingOff, AntialiasingFXAA} {
		parsed, err := ParseAntialiasingMode(mode.String())
		if err != nil || parsed != mode {
			t.Fatalf("mode round trip: %v / %v", parsed, err)
		}
		scene, err := NewScene(Config{Antialiasing: mode})
		if err != nil {
			t.Fatal(err)
		}
		fxaa := scene.fxaa
		if (fxaa != nil) != (mode == AntialiasingFXAA) {
			t.Fatal("postprocessing allocated for incorrect mode")
		}
		scene.Reset(42)
		if scene.world.antialiasing != mode || scene.fxaa != fxaa {
			t.Fatal("Reset lost antialiasing policy or postprocessing resources")
		}
		scene.Close()
		scene.Close()
	}
	if _, err := ParseAntialiasingMode("invalid"); err == nil {
		t.Fatal("invalid antialiasing flag accepted")
	}
	if _, err := NewScene(Config{Antialiasing: 255}); err == nil {
		t.Fatal("invalid antialiasing enum accepted")
	}
}

// A fresh child process isolates Ebitengine's one permitted game loop.
func TestAntialiasingPixels(t *testing.T) {
	if os.Getenv("INFINICAVE_AA_PIXELS") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAntialiasingPixels$", "-test.count=1")
		cmd.Env = append(os.Environ(), "INFINICAVE_AA_PIXELS=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("antialiasing pixels: %v\n%s", err, out)
		}
		return
	}
	ebiten.SetWindowSize(80, 80)
	if err := ebiten.RunGame(&antialiasingPixelGame{t: t}); err != nil {
		t.Fatal(err)
	}
}

type antialiasingPixelGame struct {
	t    *testing.T
	done bool
}

func (*antialiasingPixelGame) Layout(int, int) (int, int) { return 80, 80 }
func (g *antialiasingPixelGame) Update() error {
	if g.done {
		return ebiten.Termination
	}
	return nil
}
func (g *antialiasingPixelGame) Draw(*ebiten.Image) {
	if g.done {
		return
	}
	g.done = true
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		for _, mode := range []AntialiasingMode{AntialiasingFull, AntialiasingOff, AntialiasingFXAA} {
			scene, err := NewScene(Config{Antialiasing: mode, Orientation: orientation, DisableBackground: true})
			if err != nil {
				g.t.Fatal(err)
			}
			for _, size := range []image.Point{{64, 49}, {71, 57}} {
				dst := ebiten.NewImage(size.X, size.Y)
				viewport := Viewport{Y: -2, Height: 2}
				if orientation == Horizontal {
					viewport = Viewport{X: 0, Width: 2}
				}
				calls := 0
				paint := func(c RenderContext) {
					calls++
					if c.Antialiasing != mode {
						g.t.Fatal("hook lost antialiasing policy")
					}
					var path vector.Path
					path.MoveTo(10.25, 8.25)
					path.LineTo(40.25, 8.25)
					path.LineTo(10.25, 35.25)
					path.Close()
					vector.FillPath(c.Dst, &path, nil, &vector.DrawPathOptions{AntiAlias: c.Antialiasing.GeometryAA()})
				}
				scene.DrawWithPasses(dst, viewport, RenderPasses{AboveRocks: paint})
				if calls != 1 {
					g.t.Fatal("postprocessing repeated render hook")
				}
				pixels := make([]byte, 4*size.X*size.Y)
				dst.ReadPixels(pixels)
				fractional := 0
				for i := 0; i < len(pixels); i += 4 {
					a := pixels[i+3]
					if a > 0 && a < 255 {
						fractional++
					}
					if pixels[i] > a || pixels[i+1] > a || pixels[i+2] > a {
						g.t.Fatal("filter violated premultiplied alpha")
					}
				}
				if (fractional > 0) != (mode != AntialiasingOff) {
					g.t.Fatalf("%s %s: fractional edge count %d", orientation, mode, fractional)
				}
				dst.Deallocate()
			}
			if mode == AntialiasingFXAA {
				parent := ebiten.NewImage(80, 80)
				parent.Fill(color.RGBA{B: 255, A: 255})
				dst := parent.SubImage(image.Rect(7, 9, 71, 58)).(*ebiten.Image)
				viewport := Viewport{Y: -1, Height: 1}
				if orientation == Horizontal {
					viewport = Viewport{X: 0, Width: 1}
				}
				scene.DrawWithPasses(dst, viewport, RenderPasses{AboveRocks: func(c RenderContext) { c.Dst.Fill(color.White) }})
				for _, tc := range []struct {
					p    image.Point
					want color.RGBA
				}{{image.Pt(0, 0), color.RGBA{B: 255, A: 255}}, {image.Pt(7, 9), color.RGBA{255, 255, 255, 255}}, {image.Pt(70, 57), color.RGBA{255, 255, 255, 255}}} {
					if got := color.RGBAModel.Convert(parent.At(tc.p.X, tc.p.Y)).(color.RGBA); got != tc.want {
						g.t.Fatalf("subimage %v: %v want %v", tc.p, got, tc.want)
					}
				}
				parent.Deallocate()
			}
			scene.Close()
		}
	}
}

func TestBranchingConfigValidation(t *testing.T) {
	if DefaultConfig().BranchingStrategy != BranchingRecursive || (Config{}).BranchingStrategy != BranchingRecursive {
		t.Fatal("recursive must be the default, including for a zero Config")
	}
	for _, strategy := range []BranchingStrategy{BranchingSimple, BranchingRecursive, BranchingLoops} {
		parsed, err := ParseBranchingStrategy(strategy.String())
		if err != nil || parsed != strategy {
			t.Fatalf("strategy round trip: %v %v", parsed, err)
		}
	}
	if _, err := ParseBranchingStrategy("random"); err == nil {
		t.Fatal("unknown strategy accepted")
	}
	if err := (Config{BranchingStrategy: BranchingStrategy(255)}).validate(); err == nil {
		t.Fatal("invalid strategy accepted")
	}
	for _, value := range []float64{-1, 1.01, math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, c := range []Config{{SpikeDensity: value}, {EdgeRoughness: value}} {
			if c.validate() == nil {
				t.Fatalf("accepted invalid shape setting: %+v", c)
			}
		}
	}
	for _, value := range []float64{0, .5, 1} {
		if (Config{SpikeDensity: value, EdgeRoughness: value}).validate() != nil {
			t.Fatalf("rejected valid setting %g", value)
		}
	}
}

func TestSceneRockShapeMatchesGenerationAndSurvivesReset(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		for _, strategy := range []BranchingStrategy{BranchingSimple, BranchingRecursive, BranchingLoops} {
			t.Run(orientation.String()+"/"+strategy.String(), func(t *testing.T) {
				config := Config{Seed: 42, Orientation: orientation, ForceGuidesUp: true, BranchingStrategy: strategy, BranchMinThickness: .052, BranchMaxThickness: .072, SpikeDensity: .8, EdgeRoughness: .7, LoadSection: func(id int64) SectionContent {
					if id != 0 {
						return SectionContent{}
					}
					return SectionContent{Guides: []Guide{{Pts: []V{{X: .1, Y: .3}, {X: .9, Y: .3}}}}}
				}}
				if orientation == Horizontal {
					config.LoadSection = func(id int64) SectionContent {
						if id != 0 {
							return SectionContent{}
						}
						return SectionContent{Guides: []Guide{{Pts: []V{{X: .3, Y: .1}, {X: .3, Y: .9}}}}}
					}
				}
				expected, err := GenerateSectionWithConfig(config, 0)
				if err != nil {
					t.Fatal(err)
				}
				scene, err := NewScene(config)
				if err != nil {
					t.Fatal(err)
				}
				defer scene.Close()
				for attempt := 0; attempt < 2; attempt++ {
					if attempt > 0 {
						scene.Reset(config.Seed)
					}
					scene.world.request(0)
					select {
					case result := <-scene.world.terrain:
						collision := terrain.OrientedCollision(orientation, result.geometry.Collision)
						if !reflect.DeepEqual(collision, expected.Collision) {
							t.Fatal("streaming or reset changed configured rock geometry")
						}
					case <-time.After(20 * time.Second):
						t.Fatal("streamed terrain did not complete")
					}
				}
			})
		}
	}
}

func TestBranchThicknessConfigValidation(t *testing.T) {
	for _, value := range []float64{-1, 1.01, math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, c := range []Config{{BranchMinThickness: value}, {BranchMaxThickness: value}} {
			if c.validate() == nil {
				t.Fatalf("accepted invalid thickness: %+v", c)
			}
		}
	}
	for _, c := range []Config{
		{BranchMinThickness: .09, BranchMaxThickness: .05},
		{BranchMinThickness: .09},
		{BranchMaxThickness: .04},
	} {
		if c.validate() == nil {
			t.Fatalf("accepted reversed resolved bounds: %+v", c)
		}
	}
	for _, c := range []Config{
		{}, DefaultConfig(),
		{BranchMinThickness: .05}, {BranchMaxThickness: .09},
		{BranchMinThickness: .06, BranchMaxThickness: .06},
		{BranchMinThickness: .001, BranchMaxThickness: 1},
	} {
		if err := c.validate(); err != nil {
			t.Fatalf("rejected valid thickness: %+v: %v", c, err)
		}
	}
	lo, hi := (Config{}).shapeOptions().BranchThicknessBounds()
	defaults := DefaultConfig()
	if lo != defaults.BranchMinThickness || hi != defaults.BranchMaxThickness {
		t.Fatal("zero Config thickness differs from viewer defaults")
	}
}

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
	ahead, required := prefetch(forward.Y, forward.Height, forward.Velocity, DefaultConfig().LookaheadSections)
	behind, _ := prefetch(backward.Y, backward.Height, backward.Velocity, DefaultConfig().LookaheadSections)
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
