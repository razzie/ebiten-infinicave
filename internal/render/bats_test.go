package render

import (
	"math"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestBatsArriveOccasionallyFromBothSides(t *testing.T) {
	flock := NewBatFlock(42, 7.5)
	viewport := Viewport{Y: -2, Height: 1}
	left, right, arrivals := false, false, 0
	for tick := 0; tick < 120*60; tick++ {
		flock.Step(viewport, 1.0/60)
		if len(flock.Bats) > 2 {
			t.Fatal("too many bats on screen at once")
		}
		for _, b := range flock.Bats {
			if b.Age >= 1.0/60 {
				continue
			}
			arrivals++
			if tick < 60 {
				t.Fatal("bat arrived without an initial pause")
			}
			if b.Start.Y <= viewport.Y || b.Start.Y >= viewport.Y+viewport.Height {
				t.Fatalf("bat spawned outside the visible cave: %+v", b)
			}
			if b.Start.X < 0 {
				left = true
				if b.Start.X+b.Size >= terrain.BackgroundMinX || b.End.X-b.Size <= terrain.BackgroundMaxX {
					t.Fatal("left arrival must start and end offscreen on opposite sides")
				}
			} else {
				right = true
				if b.Start.X-b.Size <= terrain.BackgroundMaxX || b.End.X+b.Size >= terrain.BackgroundMinX {
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
	flock, scrolled := NewBatFlock(42, 7.5), NewBatFlock(42, 7.5)
	flock.Step(viewport, 6)
	scrolled.Step(viewport, 6)
	b := flock.Bats[0]
	if b.Position(0).Sub(b.Start).Len() > 1e-12 || b.Position(1).Sub(b.End).Len() > 1e-12 {
		t.Fatal("route did not connect its entry and exit")
	}
	previous, curved := b.Start, false
	for i := 1; i <= 100; i++ {
		u := float64(i) / 100
		point := b.Position(u)
		if (point.X-previous.X)*(b.End.X-b.Start.X) <= 0 {
			t.Fatal("bat reversed instead of crossing")
		}
		if point.Y <= viewport.Y || point.Y >= viewport.Y+viewport.Height {
			t.Fatalf("bat flew outside its original viewport: %v", point)
		}
		linearY := b.Start.Y + (b.End.Y-b.Start.Y)*(point.X-b.Start.X)/(b.End.X-b.Start.X)
		curved = curved || math.Abs(point.Y-linearY) > .02
		if math.IsNaN(b.bank(u)) || math.Abs(b.bank(u)) > .35 {
			t.Fatal("invalid banking angle")
		}
		previous = point
	}
	if !curved {
		t.Fatal("flight path stayed straight")
	}
	flock.Next, scrolled.Next = 100, 100
	flock.Step(viewport, 2)
	viewport.Y = -10
	scrolled.Step(viewport, 2)
	if !reflect.DeepEqual(flock.Bats, scrolled.Bats) {
		t.Fatal("scrolling changed an existing flight path")
	}
	flock.Next = 100
	flock.Step(viewport, b.duration)
	if len(flock.Bats) != 0 {
		t.Fatal("bats were retained after exiting the opposite side")
	}
}

func TestBatArrivalFrequency(t *testing.T) {
	viewport := Viewport{Y: -2, Height: 1}
	for _, perMinute := range []float64{.5, 7.5, 30, 120} {
		flock := NewBatFlock(42, perMinute)
		arrivals := 0
		const minutes = 20
		for tick := 0; tick < minutes*60*60; tick++ {
			flock.Step(viewport, 1.0/60)
			for _, b := range flock.Bats {
				if b.Age < 1.0/60 {
					arrivals++
				}
			}
		}
		if want := perMinute * minutes; math.Abs(float64(arrivals)-want) > math.Max(1, want*.1) {
			t.Fatalf("frequency %g: got %d arrivals in %d minutes, want about %g", perMinute, arrivals, minutes, want)
		}
	}
}

func TestBatArrivalsWithinOneUpdate(t *testing.T) {
	viewport := Viewport{Y: -2, Height: 1}
	flock := NewBatFlock(42, 7200)
	for tick := 0; tick < 60; tick++ {
		flock.Step(viewport, 1.0/60)
	}
	if len(flock.Bats) < 100 || len(flock.Bats) > 140 {
		t.Fatalf("expected about 120 arrivals in one second, got %d", len(flock.Bats))
	}
	for _, b := range flock.Bats {
		if b.Age < 0 || b.Age > 1 {
			t.Fatalf("incorrect bat age after arrival: %g", b.Age)
		}
	}
}

func TestBatCarvingHitsSilhouette(t *testing.T) {
	for _, orientation := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		for _, test := range []struct {
			name  string
			point geom.V // In the bat's unrotated silhouette, in multiples of Size.
			hit   bool
		}{
			{"body", geom.V{}, true},
			{"wing", geom.V{X: .75, Y: .08}, true},
			{"air between ear and wing", geom.V{X: .75, Y: -.5}, false},
			{"distant", geom.V{X: 3}, false},
		} {
			t.Run(orientation.String()+"/"+test.name, func(t *testing.T) {
				for _, shape := range []terrain.HoleShape{terrain.HoleCircle, terrain.HoleSegment} {
					point := geom.V{X: .5, Y: -.5}
					flock := NewBatFlock(42, 7.5, orientation)
					flock.Bats = []Bat{{Start: point, End: point, control1: point.Y, control2: point.Y, duration: 10, Size: .02}}
					local := test.point.Mul(.02)
					if orientation == terrain.Horizontal {
						local = terrain.InternalPoint(orientation, local)
					}
					center := point.Add(local)
					hole := terrain.Hole{Shape: shape, Center: center, Radius: .0001,
						Start: center.Sub(geom.V{X: .0001}), End: center.Add(geom.V{X: .0001}), Width: .0002}
					cut, err := terrain.CutFromHole(hole)
					if err != nil {
						t.Fatal(err)
					}
					flock.Carve(cut)
					if flock.Bats[0].hit != test.hit {
						t.Fatalf("shape %v: hit = %v, want %v", shape, flock.Bats[0].hit, test.hit)
					}
				}
			})
		}
	}
}

func TestHitBatFallsDownThenExpiresWithoutChangingArrivals(t *testing.T) {
	for _, orientation := range []terrain.Orientation{terrain.Vertical, terrain.Horizontal} {
		viewport := Viewport{Y: -2, Height: 1}
		flock, untouched := NewBatFlock(42, 7.5, orientation), NewBatFlock(42, 7.5, orientation)
		flock.Step(viewport, 6)
		untouched.Step(viewport, 6)
		b := flock.Bats[0]
		point := b.Position(b.Age / b.duration)
		cut, err := terrain.CutFromHole(terrain.Hole{Shape: terrain.HoleCircle, Center: point, Radius: b.Size * 2})
		if err != nil {
			t.Fatal(err)
		}
		flock.Carve(cut)
		if !flock.Bats[0].hit || flock.Bats[0].Position(0) != point {
			t.Fatal("hit did not start the fall at the current position")
		}
		// Even a bat about to exit its flight route must finish its fall and burn.
		flock.Bats[0].duration = b.Age + .001
		flock.Step(viewport, batFallDuration/2)
		untouched.Step(viewport, batFallDuration/2)
		halfway := flock.Bats[0]
		flock.Carve(cut)
		if flock.Bats[0] != halfway {
			t.Fatal("repeated hit restarted the fall")
		}
		worldDelta := terrain.WorldPoint(orientation, halfway.Position(0).Sub(point))
		if math.Abs(worldDelta.X) > 1e-12 || math.Abs(worldDelta.Y-batFallDistance/4) > 1e-12 || halfway.Age != b.Age {
			t.Fatalf("bat did not fall down with frozen wings: %+v", worldDelta)
		}
		// The camera moves and crosses a section boundary while the bat falls.
		viewport.Y = -10
		flock.Step(viewport, batFallDuration/2)
		untouched.Step(viewport, batFallDuration/2)
		landed := flock.Bats[0].Position(0)
		worldDelta = terrain.WorldPoint(orientation, landed.Sub(point))
		if math.Abs(worldDelta.X) > 1e-12 || math.Abs(worldDelta.Y-batFallDistance) > 1e-12 {
			t.Fatalf("fall distance changed with orientation or camera: %+v", worldDelta)
		}
		flock.Step(viewport, burnDuration/2)
		untouched.Step(viewport, burnDuration/2)
		if len(flock.Bats) != 1 || flock.Bats[0].Position(0) != landed {
			t.Fatal("bat did not remain for the burn after falling")
		}
		flock.Step(viewport, burnDuration/2+.001)
		untouched.Step(viewport, burnDuration/2+.001)
		if len(flock.Bats) != 0 || flock.Next != untouched.Next || flock.rng.Int63() != untouched.rng.Int63() {
			t.Fatal("expired bat remained or hits changed the arrival sequence")
		}
	}
}
