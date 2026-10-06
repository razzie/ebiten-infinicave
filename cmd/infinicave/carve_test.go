package main

import (
	"math"
	"testing"

	infinicave "github.com/razzie/ebiten-infinicave"
)

func TestBlastGestureCommitsOnlyOnRelease(t *testing.T) {
	var c carveGesture
	cursor, origin := infinicave.V{X: 200, Y: 300}, infinicave.V{X: .2, Y: -.5}
	if c.advance(cursor, origin, true, false, false, 0) {
		t.Fatal("press committed blast")
	}
	if c.advance(cursor, origin, false, false, false, 2) || c.radius != .092 {
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
	c.advance(cursor, origin.Add(infinicave.V{Y: -.1}), false, false, false, 1)
	if c.segment || c.origin != origin {
		t.Fatal("camera movement changed blast mode/origin")
	}
	c.advance(cursor.Add(infinicave.V{X: 1}), origin.Add(infinicave.V{X: .001}), false, false, false, 0)
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

func TestViewerWorldPointMatchesRoundedDrawOrigin(t *testing.T) {
	for _, width := range []int{500, 1000, 1500} {
		p := viewerWorldPoint(infinicave.V{X: float64(width) * .25, Y: float64(width) * .125}, -.8004, width)
		if p.X != .25 || math.Abs(p.Y-(.125+math.Round(-.8004*float64(width))/float64(width))) > 1e-12 {
			t.Fatalf("world cursor at width %d: %v", width, p)
		}
	}
}
