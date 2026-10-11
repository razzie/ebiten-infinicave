package terrain

import (
	"math"
	"reflect"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestSpiderWebNetworkConnectsSilkToFixedRockAnchors(t *testing.T) {
	for _, anchors := range [][]geom.V{
		{{X: .2, Y: .3}, {X: .34, Y: .3}, {X: .23, Y: .40}},
		{{X: .2, Y: .3}, {X: .4, Y: .3}, {X: .24, Y: .36}, {X: .30, Y: .3}, {X: .22, Y: .33}},
		{{X: .2, Y: .3}, {X: .32, Y: .3}, {X: .2, Y: .4}, {X: .33, Y: .42}},
	} {
		for _, variation := range []float64{0, .35, .8, 1} {
			w := NewSpiderWeb(anchors, variation)
			n := w.Network
			if n == nil {
				t.Fatal("usable attachments did not produce a web")
			}
			if !reflect.DeepEqual(anchors, n.Nodes[:n.Anchors]) {
				t.Fatal("relaxation moved a rock attachment")
			}
			adjacent := make([][]int, len(n.Nodes))
			kinds := make(map[WebThreadKind]int)
			for _, silk := range n.Threads {
				adjacent[silk.A] = append(adjacent[silk.A], silk.B)
				adjacent[silk.B] = append(adjacent[silk.B], silk.A)
				kinds[silk.Kind]++
			}
			seen := make([]bool, len(n.Nodes))
			queue := []int{0}
			seen[0] = true
			for len(queue) > 0 {
				at := queue[0]
				queue = queue[1:]
				for _, next := range adjacent[at] {
					if !seen[next] {
						seen[next] = true
						queue = append(queue, next)
					}
				}
			}
			for i, p := range n.Nodes {
				if !seen[i] || math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) {
					t.Fatalf("node %d is disconnected or nonfinite", i)
				}
				if i >= n.Anchors && len(adjacent[i]) < 2 {
					t.Fatal("free silk endpoint has no structural support")
				}
			}
			if kinds[WebSupport] != len(anchors) || kinds[WebCapture] < 40 || len(n.Hub) < 8 {
				t.Fatal("missing attachment, capture, or connected hub structure")
			}
			frame := webFramePoints(n)
			for _, node := range n.Hub {
				if !geom.InsidePolygon(n.Nodes[node], frame) {
					t.Fatal("hub escaped its supporting frame")
				}
			}
			// Frame and capture silk must meet at shared nodes rather than
			// cross without a junction. Detect unintended intersections.
			for i, a := range n.Threads {
				for _, b := range n.Threads[i+1:] {
					if a.A == b.A || a.A == b.B || a.B == b.A || a.B == b.B {
						continue
					}
					if geom.SegmentDistanceSquared(n.Nodes[a.A], n.Nodes[a.B], n.Nodes[b.A], n.Nodes[b.B]) < 1e-22 {
						t.Fatalf("unconnected threads cross: %+v, %+v", a, b)
					}
				}
			}
			if !reflect.DeepEqual(w, NewSpiderWeb(anchors, variation)) {
				t.Fatal("same attachments changed the resting network")
			}
		}
	}
}

func TestSpiderWebNetworkStretchesCaptureAreaToTheOpening(t *testing.T) {
	anchors := []geom.V{{}, {X: .22}, {X: .015, Y: .060}, {X: .10}, {X: .009, Y: .036}}
	n := NewSpiderWeb(anchors, .5).Network
	if n == nil {
		t.Fatal("elongated opening did not produce a web")
	}
	lo, hi := geom.V{X: 1, Y: 1}, geom.V{X: -1, Y: -1}
	for _, t := range n.Threads {
		if t.Kind != WebCapture {
			continue
		}
		for _, node := range []int{t.A, t.B} {
			p := n.Nodes[node]
			lo.X, lo.Y = min(lo.X, p.X), min(lo.Y, p.Y)
			hi.X, hi.Y = max(hi.X, p.X), max(hi.Y, p.Y)
		}
	}
	if (hi.X-lo.X)/(hi.Y-lo.Y) < 2 {
		t.Fatal("elongated attachments were reduced to a circular capture area")
	}
	// Unsupported frame spans bow toward the capture area.
	frame := webFramePoints(n)
	concave := 0
	for i, p := range frame {
		a, b := frame[(i+len(frame)-1)%len(frame)], frame[(i+1)%len(frame)]
		if geom.Cross(p.Sub(a), b.Sub(p)) < -1e-9 {
			concave++
		}
	}
	if concave < 3 {
		t.Fatal("frame has no inward pull between its supports")
	}
}

func TestSpiderWebTensionOnlyShortensExtendedStrands(t *testing.T) {
	n := &WebNetwork{Anchors: 1, Nodes: []geom.V{{}, {X: 2}, {X: .5}}, Threads: []WebThread{{0, 1, WebSupport}, {0, 2, WebSupport}}}
	relaxWebNetwork(n, []webConstraint{{1, 1}, {1, 1}})
	if n.Nodes[0] != (geom.V{}) || n.Nodes[1] != (geom.V{X: 1}) || n.Nodes[2] != (geom.V{X: .5}) {
		t.Fatal("silk moved a pinned anchor or pushed a slack strand outward")
	}
}

func TestSpiderWebNetworkRejectsCollapsedAttachments(t *testing.T) {
	for _, anchors := range [][]geom.V{nil, {{}, {X: .1}}, {{}, {}, {}}, {{}, {X: .1}, {X: .2}}, {{}, {X: .001}, {Y: .001}}} {
		if NewSpiderWeb(anchors, .5).Network != nil {
			t.Fatal("collapsed attachments produced silk")
		}
	}
}

func TestSpiderWebClearanceRejectsThinObstructionsAndAllowsAttachments(t *testing.T) {
	grid := RockGrid{terrainRect(.4, .1, .000001, .8)}
	c := newWebClearance(grid)
	faces := []int{0}
	if c.segmentClear(geom.V{X: .1, Y: .5}, geom.V{X: .9, Y: .5}, faces) {
		t.Fatal("thin rock between sample points did not block silk")
	}
	if !c.segmentClear(geom.V{X: .1, Y: .5}, geom.V{X: .4, Y: .5}, faces) {
		t.Fatal("an intended boundary attachment was rejected")
	}
	if c.segmentClear(geom.V{X: .1, Y: .5}, geom.V{X: .5, Y: .5}, faces) {
		t.Fatal("a strand entering rock behind its anchor was accepted")
	}
}

func TestSpiderWebNetworkIsInvariantUnderSectionTranslation(t *testing.T) {
	anchors := []geom.V{{X: .2, Y: .4}, {X: .34, Y: .4}, {X: .24, Y: .49}}
	n := NewSpiderWeb(anchors, .7).Network
	for i := range anchors {
		anchors[i].Y++
	}
	m := NewSpiderWeb(anchors, .7).Network
	if n == nil || m == nil || !reflect.DeepEqual(n.Threads, m.Threads) {
		t.Fatal("translation changed the network structure")
	}
	for i, p := range n.Nodes {
		if p.Sub(m.Nodes[i].Sub(geom.V{Y: 1})).Len() > 1e-12 {
			t.Fatal("translation changed the settled silk geometry")
		}
	}
}
