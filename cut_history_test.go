package infinicave

import (
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestCutHistorySpatialReplayAndHugeCuts(t *testing.T) {
	w := &world{}
	for _, h := range []Hole{
		{Shape: HoleCircle, Center: V{X: .5, Y: -.5}, Radius: .05},
		{Shape: HoleCircle, Center: V{X: .5, Y: -5000.5}, Radius: .05},
		{Shape: HoleCircle, Center: V{X: .5, Y: -100.5}, Radius: 200},
		{Shape: HoleSegment, Start: V{X: .2, Y: -1}, End: V{X: .8, Y: -1}, Width: .03},
	} {
		cut, err := terrain.CutFromHole(h)
		if err != nil {
			t.Fatal(err)
		}
		w.appendCut(h, cut)
	}
	if got := w.cutsForSection(0, 0); !reflect.DeepEqual(got, []int{0, 2, 3}) {
		t.Fatalf("nearby replay lost chronological ordering: %v", got)
	}
	if got := w.cutsForSection(5000, 0); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("distant window replayed unrelated cuts: %v", got)
	}
	if got := w.cutsForSection(0, 3); !reflect.DeepEqual(got, []int{3}) {
		t.Fatalf("revision replayed old edits: %v", got)
	}
	if len(w.cutBuckets) > 10 || len(w.largeCuts) != 1 {
		t.Fatal("large edit expanded into an unbounded bucket range")
	}
	for _, c := range w.cuts {
		cut := c.expand()
		if cut.Min != c.min || cut.Max != c.max {
			t.Fatal("compact command changed exact cut bounds")
		}
	}
}
