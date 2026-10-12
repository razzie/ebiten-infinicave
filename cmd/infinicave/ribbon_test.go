package main

import (
	"flag"
	"math"
	"os"
	"reflect"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave"
)

func TestRibbonOptions(t *testing.T) {
	oldFlags, oldArgs := flag.CommandLine, os.Args
	t.Cleanup(func() { flag.CommandLine, os.Args = oldFlags, oldArgs })
	for _, tc := range []struct {
		value   string
		want    ribbonMode
		invalid bool
	}{
		{"", ribbonOff, false}, {"off", ribbonOff, false}, {"back", ribbonBack, false}, {"front", ribbonFront, false}, {"above", ribbonOff, true},
	} {
		flag.CommandLine = flag.NewFlagSet("ribbon", flag.ContinueOnError)
		os.Args = []string{"infinicave"}
		if tc.value != "" {
			os.Args = append(os.Args, "-ribbon", tc.value)
		}
		options, err := parseOptions()
		if (err != nil) != tc.invalid || (!tc.invalid && options.ribbon != tc.want) {
			t.Fatalf("ribbon %q: mode=%v err=%v", tc.value, options.ribbon, err)
		}
	}
}

func TestRibbonDrawingFollowsWorldCursorWithoutAdvancing(t *testing.T) {
	r := newRibbonTrail(ribbonFront)
	t.Cleanup(r.close)
	for _, p := range []infinicave.V{{X: .1, Y: -.6}, {X: .2, Y: -.55}, {X: .3, Y: -.5}, {X: .4, Y: -.5}} {
		r.step(p, true, 1.0/60)
	}
	dst := ebiten.NewImage(100, 100)
	t.Cleanup(dst.Deallocate)
	ctx := infinicave.RenderContext{Dst: dst}
	ctx.WorldToTarget.Translate(0, 1)
	ctx.WorldToTarget.Scale(100, 100)
	before := append([]ribbonPoint(nil), r.points...)
	for draw := 0; draw < 2; draw++ {
		r.draw(ctx)
		if !reflect.DeepEqual(before, r.points) {
			t.Fatal("drawing advanced ribbon animation")
		}
		for _, v := range r.vertices {
			if math.IsNaN(float64(v.DstX)) || math.IsNaN(float64(v.DstY)) || math.IsInf(float64(v.DstX), 0) || math.IsInf(float64(v.DstY), 0) {
				t.Fatal("ribbon has nonfinite geometry")
			}
		}
		for _, index := range r.indices {
			if int(index) >= len(r.vertices) {
				t.Fatal("ribbon has an invalid triangle")
			}
		}
		a, b := r.vertices[len(r.vertices)-2], r.vertices[len(r.vertices)-1]
		if math.Abs(float64(a.DstX+b.DstX)/2-40) > 1e-5 || math.Abs(float64(a.DstY+b.DstY)/2-50) > 1e-5 {
			t.Fatal("ribbon head does not follow the world cursor")
		}
	}
}

func TestRibbonTrailAgesAndStaysBounded(t *testing.T) {
	r := &ribbonTrail{}
	for i := 0; i < 200; i++ {
		r.step(infinicave.V{X: float64(i) * .001, Y: -.5}, true, .001)
	}
	if len(r.points) != ribbonMaxPoints || r.points[len(r.points)-1].position.X != .199 {
		t.Fatal("trail lost its cursor head or exceeded the sample limit")
	}
	last := r.points[len(r.points)-1].position
	r.step(last, true, .5)
	if r.points[len(r.points)-1].age != 0 {
		t.Fatal("stationary cursor lost the ribbon head")
	}
	r.step(last, false, ribbonLifetime)
	if len(r.points) != 0 {
		t.Fatal("trail remained after leaving the window")
	}
	for _, mode := range []ribbonMode{ribbonOff, ribbonBack, ribbonFront} {
		r.mode = mode
		passes := r.passes()
		if (passes.BehindRocks != nil) != (mode == ribbonBack) || (passes.AboveRocks != nil) != (mode == ribbonFront) {
			t.Fatal("ribbon selected the wrong pass")
		}
	}
}
