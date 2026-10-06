package infinicave

import (
	"math"
	"reflect"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
)

func TestBatsConfigurationAndLifecycle(t *testing.T) {
	configs := []Config{{}, DefaultConfig(), {Bats: true}}
	for view := ViewClay; view <= ViewShadows; view++ {
		configs = append(configs, Config{Bats: true, View: view})
	}
	for _, config := range configs {
		scene, err := NewScene(config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(scene.Close)
		if enabled := config.Bats && config.View == ViewShaded; (scene.bats != nil) != enabled {
			t.Fatalf("bats enabled incorrectly for config %+v", config)
		}
		if scene.bats == nil {
			scene.Reset(42)
			if scene.bats != nil {
				t.Fatal("reset enabled disabled bats")
			}
			continue
		}
		flock := scene.bats
		before := flock.next
		scene.Update(Viewport{})
		if flock.next != before {
			t.Fatal("invalid viewport advanced animation")
		}
		scene.Update(Viewport{Y: -.8, Height: .8})
		if flock.next >= before {
			t.Fatal("valid update did not advance animation while terrain loads")
		}
		flock.step(Viewport{Y: -.8, Height: .8}, 6)
		flock.step(Viewport{Y: -.8, Height: .8}, 1)
		image := ebiten.NewImage(100, 80)
		before, bats := flock.next, append([]bat(nil), flock.bats...)
		scene.Draw(image, Viewport{Y: -.8, Height: .8})
		scene.SetRenderWidth(200)
		scene.Draw(image, Viewport{Y: -.8, Height: .8})
		image.Deallocate()
		if flock.next != before || !reflect.DeepEqual(flock.bats, bats) {
			t.Fatal("repeated draws or resizing advanced animation")
		}
		scene.Reset(42)
		if scene.bats == nil || len(scene.bats.bats) != 0 {
			t.Fatal("reset did not clear bats while keeping them enabled")
		}
		fresh := newBatFlock(42)
		if scene.bats.next != fresh.next {
			t.Fatal("reset did not restart the animation with the new seed")
		}
		scene.Close()
		scene.Update(Viewport{Y: -.8, Height: .8})
		if scene.bats != nil {
			t.Fatal("closed scene retained bats")
		}
	}
}

func TestBatsArriveOccasionallyFromBothSides(t *testing.T) {
	flock := newBatFlock(42)
	viewport := Viewport{Y: -2, Height: 1}
	left, right, arrivals := false, false, 0
	for tick := 0; tick < 120*60; tick++ {
		flock.step(viewport, 1.0/60)
		if len(flock.bats) > 2 {
			t.Fatal("too many bats on screen at once")
		}
		for _, b := range flock.bats {
			if b.age != 0 {
				continue
			}
			arrivals++
			if tick < 60 {
				t.Fatal("bat arrived without an initial pause")
			}
			if b.start.Y <= viewport.Y || b.start.Y >= viewport.Y+viewport.Height {
				t.Fatalf("bat spawned outside the visible cave: %+v", b)
			}
			if b.start.X < 0 {
				left = true
				if b.start.X+b.size >= 0 || b.end.X-b.size <= Width {
					t.Fatal("left arrival must start and end offscreen on opposite sides")
				}
			} else {
				right = true
				if b.start.X-b.size <= Width || b.end.X+b.size >= 0 {
					t.Fatal("right arrival must start and end offscreen on opposite sides")
				}
			}
		}
	}
	if !left || !right || arrivals < 10 || arrivals > 25 {
		t.Fatalf("expected occasional arrivals from both sides: left=%v right=%v arrivals=%d", left, right, arrivals)
	}
}

func TestBatRoutesCurveAndCrossWithoutFollowingCamera(t *testing.T) {
	viewport := Viewport{Y: -2, Height: 1}
	flock, scrolled := newBatFlock(42), newBatFlock(42)
	flock.step(viewport, 6)
	scrolled.step(viewport, 6)
	b := flock.bats[0]
	if b.position(0).Sub(b.start).Len() > 1e-12 || b.position(1).Sub(b.end).Len() > 1e-12 {
		t.Fatal("route did not connect its entry and exit")
	}
	previous, curved := b.start, false
	for i := 1; i <= 100; i++ {
		u := float64(i) / 100
		point := b.position(u)
		if (point.X-previous.X)*(b.end.X-b.start.X) <= 0 {
			t.Fatal("bat reversed instead of crossing")
		}
		if point.Y <= viewport.Y || point.Y >= viewport.Y+viewport.Height {
			t.Fatalf("bat flew outside its original viewport: %v", point)
		}
		linearY := b.start.Y + (b.end.Y-b.start.Y)*(point.X-b.start.X)/(b.end.X-b.start.X)
		curved = curved || math.Abs(point.Y-linearY) > .02
		if math.IsNaN(b.bank(u)) || math.Abs(b.bank(u)) > .35 {
			t.Fatal("invalid banking angle")
		}
		previous = point
	}
	if !curved {
		t.Fatal("flight path stayed straight")
	}
	flock.next, scrolled.next = 100, 100
	flock.step(viewport, 2)
	viewport.Y = -10
	scrolled.step(viewport, 2)
	if !reflect.DeepEqual(flock.bats, scrolled.bats) {
		t.Fatal("scrolling changed an existing flight path")
	}
	flock.step(viewport, 8)
	if len(flock.bats) != 0 {
		t.Fatal("bats were retained after exiting the opposite side")
	}
}
