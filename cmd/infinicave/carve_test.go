package main

import (
	"math"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	infinicave "github.com/razzie/ebiten-infinicave"
)

func TestBlastGestureCommitsOnlyOnRelease(t *testing.T) {
	var c carveGesture
	cursor, origin := infinicave.V{X: 200, Y: 300}, infinicave.V{X: .2, Y: -.5}
	if c.advance(cursor, origin, true, false, false, 0) {
		t.Fatal("press committed blast")
	}
	if c.advance(cursor, origin, false, false, false, 2) || c.radius <= blastStartRadius {
		t.Fatalf("hold should only grow preview: %+v", c)
	}
	if !c.advance(cursor, origin, false, true, false, 0) || c.active || c.segment || c.origin != origin {
		t.Fatalf("release did not commit blast: %+v", c)
	}
	if c.advance(cursor, origin, false, true, false, 0) {
		t.Fatal("release committed twice")
	}
}

func TestSegmentGestureLatchesMovementAndCancellation(t *testing.T) {
	var c carveGesture
	cursor, origin := infinicave.V{X: 200, Y: 300}, infinicave.V{X: .2, Y: -.5}
	c.advance(cursor, origin, true, false, false, 0)
	// Moving the camera without moving the cursor keeps blast mode and origin.
	c.advance(cursor, origin.Add(infinicave.V{Y: -.1}), false, false, false, .1)
	if c.segment || c.origin != origin {
		t.Fatal("camera movement changed blast mode/origin")
	}
	c.advance(cursor.Add(infinicave.V{X: drillDragPixels}), origin.Add(infinicave.V{X: .008}), false, false, false, 0)
	if !c.segment {
		t.Fatal("cursor movement did not switch to segment")
	}
	c.advance(cursor, origin, false, false, false, 0)
	if !c.segment {
		t.Fatal("returning to origin switched back to blast")
	}
	if c.advance(cursor, origin, false, true, true, 0) || c.active {
		t.Fatal("cancel did not win over release")
	}
	if c.advance(cursor, origin, false, false, false, 1) || c.advance(cursor, origin, false, true, false, 0) {
		t.Fatal("cancelled hold/release restarted the cut")
	}
	c.advance(cursor, origin, true, false, false, 0)
	end := origin.Add(infinicave.V{X: .1})
	if !c.advance(cursor.Add(infinicave.V{X: 100}), end, false, true, false, 0) || !c.segment || c.end != end {
		t.Fatal("movement on release was not included in segment")
	}
}

func TestBlastGestureIgnoresCursorDrift(t *testing.T) {
	var c carveGesture
	cursor, origin := infinicave.V{X: 200, Y: 300}, infinicave.V{X: .2, Y: -.5}
	c.advance(cursor, origin, true, false, false, 0)
	for _, delta := range []infinicave.V{{X: .25, Y: .25}, {X: 7}, {X: -7}, {Y: 7}, {X: 5, Y: 5}, {}} {
		c.advance(cursor.Add(delta), origin.Add(delta.Mul(.001)), false, false, false, 1.0/60)
		if c.segment || c.origin != origin || c.end != origin {
			t.Fatalf("cursor drift %v changed blast mode or position: %+v", delta, c)
		}
	}
	if c.radius <= .012 {
		t.Fatal("blast stopped growing during cursor drift")
	}
	if !c.advance(cursor.Add(infinicave.V{X: 7}), origin, false, true, false, 0) || c.segment {
		t.Fatal("small movement on release changed blast mode")
	}
	// Diagonal movement uses distance from the press, not separate axis thresholds.
	c.advance(cursor, origin, true, false, false, 0)
	c.advance(cursor.Add(infinicave.V{X: 6, Y: 6}), origin.Add(infinicave.V{X: .006, Y: .006}), false, false, false, 0)
	if !c.segment {
		t.Fatal("diagonal movement beyond the dead zone did not select a drill")
	}
}

func TestBlastGestureLocksAtRadiusThreshold(t *testing.T) {
	for _, radius := range []float64{blastLockRadius - .001, blastLockRadius, blastLockRadius + .001} {
		var c carveGesture
		cursor, origin := infinicave.V{X: 200, Y: 300}, infinicave.V{X: .2, Y: -.5}
		c.advance(cursor, origin, true, false, false, 0)
		c.radius = radius
		movedCursor, movedWorld := cursor.Add(infinicave.V{X: 100}), origin.Add(infinicave.V{X: .1})
		c.advance(movedCursor, movedWorld, false, false, false, 1)
		wantSegment := radius < blastLockRadius
		if c.segment != wantSegment {
			t.Fatalf("radius %v: segment = %v, want %v", radius, c.segment, wantSegment)
		}
		if !wantSegment && (c.radius <= radius || c.origin != origin || c.end != origin) {
			t.Fatalf("locked blast stopped growing or moved: %+v", c)
		}
		if !c.advance(movedCursor, movedWorld, false, true, false, 0) || c.segment != wantSegment {
			t.Fatalf("radius %v: release changed carve mode: %+v", radius, c)
		}
		// A fresh press must allow dragging again after a locked blast.
		c.advance(cursor, origin, true, false, false, 0)
		c.advance(movedCursor, movedWorld, false, false, false, 0)
		if !c.segment {
			t.Fatal("new gesture retained the blast lock")
		}
	}
}

func TestBlastGestureLocksAfterShortHold(t *testing.T) {
	var c carveGesture
	cursor, origin := infinicave.V{X: 200, Y: 300}, infinicave.V{X: .2, Y: -.5}
	c.advance(cursor, origin, true, false, false, 0)
	for range 15 {
		c.advance(cursor, origin, false, false, false, 1.0/60)
	}
	if c.radius >= blastLockRadius {
		t.Fatal("short hold should still have a small circle with slow exponential growth")
	}
	movedCursor, movedWorld := cursor.Add(infinicave.V{X: 100}), origin.Add(infinicave.V{X: .1})
	if !c.advance(movedCursor, movedWorld, false, true, false, 0) || c.segment || c.origin != origin {
		t.Fatal("movement after a short hold did not commit a circle at the initial point")
	}
}

func TestBlastGrowthAcceleratesAcrossTickRates(t *testing.T) {
	cursor, origin := infinicave.V{X: 200, Y: 300}, infinicave.V{X: .2, Y: -.5}
	for _, ticksPerSecond := range []int{1, 30, 60, 120} {
		var c carveGesture
		c.advance(cursor, origin, true, false, false, 0)
		startRadius := c.radius
		for range 2 * ticksPerSecond {
			c.advance(cursor, origin, false, false, false, 1/float64(ticksPerSecond))
		}
		wantRadius := .012 + .04/math.Ln2
		if math.Abs(c.radius-wantRadius) > 1e-12 {
			t.Fatalf("%d ticks per second: radius after two seconds = %v, want %v", ticksPerSecond, c.radius, wantRadius)
		}
		firstGrowth := c.radius - startRadius
		previousRadius := c.radius
		for range 2 * ticksPerSecond {
			c.advance(cursor, origin, false, false, false, 1/float64(ticksPerSecond))
		}
		if math.Abs((c.radius-previousRadius)-2*firstGrowth) > 1e-12 {
			t.Fatalf("%d ticks per second: growth did not double over the next two seconds", ticksPerSecond)
		}
	}
}

func TestBlastGrowthStartsPromptly(t *testing.T) {
	var c carveGesture
	cursor, origin := infinicave.V{X: 200, Y: 300}, infinicave.V{X: .2, Y: -.5}
	c.advance(cursor, origin, true, false, false, 0)
	c.advance(cursor, origin, false, false, false, .1)
	if c.radius < .014 || c.radius > .0141 {
		t.Fatalf("initial growth should be about .02 scene units per second, radius = %v", c.radius)
	}
}

func TestCarveStatusExpiresAndNewMessageRestartsTimeout(t *testing.T) {
	g := &Game{}
	g.setCarveStatus("previous cut")
	seconds := 1 / float64(ebiten.TPS())
	g.updateCarving()
	if g.carveStatus != "previous cut" || math.Abs(g.carveStatusRemaining-(carveStatusSeconds-seconds)) > 1e-12 {
		t.Fatal("status disappeared early or countdown did not advance")
	}
	g.carveStatusRemaining = seconds / 2
	g.updateCarving()
	if g.carveStatus != "" || g.carveStatusRemaining != 0 {
		t.Fatal("expired status was not cleared")
	}
	g.setCarveStatus("next cut")
	g.updateCarving()
	g.setCarveStatus("new cut")
	if g.carveStatus != "new cut" || g.carveStatusRemaining != carveStatusSeconds {
		t.Fatal("new status did not restart the timeout")
	}
	g.setCarveStatus("")
	if g.carveStatusRemaining != 0 {
		t.Fatal("clearing status retained its countdown")
	}
}

func TestViewerWorldPointMatchesRoundedDrawOrigin(t *testing.T) {
	for _, width := range []int{500, 1000, 1500} {
		p := viewerWorldPoint(infinicave.V{X: float64(width) * .25, Y: float64(width) * .125}, -.8004, width)
		if p.X != .25 || math.Abs(p.Y-(.125+math.Round(-.8004*float64(width))/float64(width))) > 1e-12 {
			t.Fatalf("world cursor at width %d: %v", width, p)
		}
	}
}
