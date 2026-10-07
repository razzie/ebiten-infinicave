package main

import (
	"testing"
	"time"

	infinicave "github.com/razzie/ebiten-infinicave"
)

func TestHoverUsesQueriesAndInvalidatesGeometryCache(t *testing.T) {
	scene, err := infinicave.NewScene(infinicave.Config{
		Seed: 42, View: infinicave.ViewClay,
		LoadSection: func(id int64) infinicave.SectionContent {
			if id != 0 {
				return infinicave.SectionContent{}
			}
			return infinicave.SectionContent{Guides: []infinicave.Guide{{
				Pts: []infinicave.V{{X: .2, Y: .5}, {X: .8, Y: .5}}, Seed: 1,
			}}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer scene.Close()
	scene.SetRenderWidth(100)
	viewport := infinicave.Viewport{Y: -.80049, Height: .8}
	deadline := time.Now().Add(20 * time.Second)
	for !scene.Update(viewport) {
		if time.Now().After(deadline) {
			t.Fatal("hover fixture failed to load")
		}
		time.Sleep(time.Millisecond)
	}
	r, err := newHoverRenderer()
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	// Rendering rounds the camera to -.8 at this resolution. The guide
	// occupies world Y=-.5, at viewport Y=.3, and wins over adjacent rock.
	target := hoverAt(scene, infinicave.V{X: .5, Y: .3}, viewport, 100)
	if target.kind != infinicave.TargetGuide {
		t.Fatalf("guide hover did not match the rounded render origin: %+v", target)
	}
	r.selectTarget(target, scene, 100, 0, 0)
	if r.image == nil {
		t.Fatal("guide selection did not build an overlay")
	}
	image := r.image
	other := hoverAt(scene, infinicave.V{X: .6, Y: .3}, viewport, 100)
	r.selectTarget(other, scene, 100, 0, 0)
	if r.image != image {
		t.Fatal("moving along a guide rebuilt the overlay")
	}
	r.selectTarget(target, scene, 500, 0, 0)
	if r.image == image || r.pixels != 500 {
		t.Fatal("resize retained an overlay at the old resolution")
	}
	image = r.image
	r.selectTarget(target, scene, 500, 0, 1)
	if r.image == image {
		t.Fatal("visible band changes retained the old crop")
	}
	// Find solid rock away from the guide, then edit it. Even though the
	// selected guide's ID is unchanged, the scene revision must invalidate it.
	var rock infinicave.QueryResult
	for y := -.7; y < -.2 && !rock.Found; y += .02 {
		if y > -.52 && y < -.48 {
			continue
		}
		for x := .25; x < .75 && !rock.Found; x += .02 {
			rock, err = scene.Query(infinicave.Ray{Origin: infinicave.V{X: x, Y: y}},
				infinicave.QueryOptions{Targets: infinicave.TargetRock})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if !rock.Found {
		t.Fatal("fixture has no rock to carve")
	}
	image = r.image
	if result, err := scene.CarveCircle(rock.Hit.Point, .01); err != nil || len(result.SectionIDs) == 0 {
		t.Fatalf("fixture carve failed: %+v, %v", result, err)
	}
	r.selectTarget(target, scene, 500, 0, 1)
	if r.image == image || r.revision != scene.GeometryRevision() {
		t.Fatal("geometry revision did not invalidate the overlay")
	}
	oldRock := hoverTarget{kind: infinicave.TargetRock, formation: rock.Hit.FormationID}
	r.selectTarget(oldRock, scene, 500, 0, 0)
	if r.image != nil || r.target.kind != 0 {
		t.Fatal("expired formation retained a highlight")
	}
	for _, cursor := range []infinicave.V{
		{X: -.001, Y: .3}, {X: 1, Y: .3}, {X: .5, Y: -.001}, {X: .5, Y: .8},
	} {
		if got := hoverAt(scene, cursor, viewport, 100); got.kind != 0 {
			t.Fatalf("out-of-view cursor selected geometry: %+v", cursor)
		}
	}
	if got := hoverAt(scene, infinicave.V{X: .5, Y: .3},
		infinicave.Viewport{Y: -100, Height: .8}, 100); got.kind != 0 {
		t.Fatal("unloaded terrain retained a selection")
	}
	r.selectTarget(target, scene, 500, 0, 0)
	scene.Reset(7)
	r.selectTarget(target, scene, 500, 0, 0)
	if r.image != nil || r.target.kind != 0 {
		t.Fatal("reset retained an expired guide highlight")
	}
}

func TestHoverRasterBoundsLimitLongFormations(t *testing.T) {
	r, err := newHoverRenderer()
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	r.target = hoverTarget{kind: infinicave.TargetRock}
	r.pixels, r.low, r.high = 1000, 0, 0
	// A formation spanning a hundred sections only needs a viewport-sized
	// overlay. Concave loops and holes are passed through to the winding fill.
	r.render([][]infinicave.V{
		{{X: .1, Y: -100}, {X: .3, Y: -100}, {X: .3, Y: -.1}, {X: .1, Y: -.1}},
		{{X: .15, Y: -.6}, {X: .15, Y: -.4}, {X: .25, Y: -.4}, {X: .25, Y: -.6}},
	})
	if r.image == nil || r.image.Bounds().Dx() != 232 || r.image.Bounds().Dy() != 932 {
		t.Fatal("long formation was not cropped to the visible section band")
	}
	if r.origin != (infinicave.V{X: .084, Y: -1.016}) {
		t.Fatalf("overlay origin lost world/pixel alignment: %+v", r.origin)
	}
	r.clear()
	if r.image != nil || r.target.kind != 0 {
		t.Fatal("empty selection retained the overlay")
	}
}

func TestLandscapeHoverUsesWorldCoordinates(t *testing.T) {
	scene, err := infinicave.NewScene(infinicave.Config{
		Seed: 42, Orientation: infinicave.Horizontal, View: infinicave.ViewClay,
		LoadSection: func(id int64) infinicave.SectionContent {
			if id != 0 {
				return infinicave.SectionContent{}
			}
			return infinicave.SectionContent{Guides: []infinicave.Guide{{Pts: []infinicave.V{{X: .2, Y: .5}, {X: .8, Y: .5}}}}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(scene.Close)
	scene.SetRenderWidth(100)
	viewport := infinicave.Viewport{X: .00049, Width: 1.8}
	deadline := time.Now().Add(20 * time.Second)
	for !scene.Update(viewport) {
		if time.Now().After(deadline) {
			t.Fatal("landscape hover fixture did not load")
		}
		time.Sleep(time.Millisecond)
	}
	target := hoverAt(scene, infinicave.V{X: .5, Y: .5}, viewport, 100)
	if target.kind != infinicave.TargetGuide {
		t.Fatalf("landscape guide hover disagrees with the raster origin: %+v", target)
	}
	r, err := newHoverRenderer()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.close)
	r.selectTarget(target, scene, 100, 0, 1)
	if r.image == nil || r.image.Bounds().Dx() < r.image.Bounds().Dy() || r.orientation != infinicave.Horizontal {
		t.Fatal("landscape guide highlight has incorrect world bounds")
	}
	for _, cursor := range []infinicave.V{{X: -.001, Y: .5}, {X: 1.8, Y: .5}, {X: .5, Y: -.001}, {X: .5, Y: 1}} {
		if got := hoverAt(scene, cursor, viewport, 100); got.kind != 0 {
			t.Fatalf("landscape margin selected terrain: %v", cursor)
		}
	}
}
