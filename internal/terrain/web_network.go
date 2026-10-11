package terrain

import (
	"math"
	"sort"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// WebThreadKind separates load-bearing silk from the finer capture threads.
type WebThreadKind uint8

const (
	WebSupport WebThreadKind = iota
	WebFrame
	WebRadial
	WebCapture
	WebHub
)

// WebThread joins shared nodes, so silk intersections cannot drift apart.
type WebThread struct {
	A, B int
	Kind WebThreadKind
}

// WebNetwork is a settled, immutable decoration in the padded section frame.
// The first Anchors nodes are fixed to rock; Frame and Hub index free nodes.
type WebNetwork struct {
	Nodes   []geom.V
	Threads []WebThread
	Anchors int
	Frame   []int
	Hub     []int
}

// NewSpiderWeb builds and relaxes a web from rock attachments. Terrain
// placement additionally checks its finished strands against the rock faces.
func NewSpiderWeb(anchors []geom.V, variation float64) SpiderWeb {
	w := SpiderWeb{Variation: variation}
	if len(anchors) < 3 {
		return w
	}
	w.Corner, w.A, w.B = anchors[0], anchors[1], anchors[2]
	w.Network = buildWebNetwork(anchors, variation)
	return w
}

type webConstraint struct {
	rest, stiffness float64
}

func buildWebNetwork(anchors []geom.V, variation float64) *WebNetwork {
	if len(anchors) < 3 {
		return nil
	}
	// Work relative to an attachment, so section translation does not affect
	// the solver or the pattern's deterministic random channels.
	origin := anchors[0]
	n := &WebNetwork{Anchors: len(anchors)}
	for _, p := range anchors {
		if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) {
			return nil
		}
		n.Nodes = append(n.Nodes, p.Sub(origin))
	}
	random := func(channel int64) float64 {
		return SiteRandom(0x73696c6b, int64(math.Round(variation*1e7)), 0, channel)
	}
	// Unequal barycentric weights move the hub away from a repeated center.
	hub := geom.V{}
	total := 0.0
	for i, p := range n.Nodes {
		weight := .7 + .6*random(int64(i))
		hub = hub.Add(p.Mul(weight))
		total += weight
	}
	hub = hub.Mul(1 / total)
	type attachment struct {
		node  int
		angle float64
		p     geom.V
	}
	ordered := make([]attachment, len(anchors))
	for i, p := range n.Nodes {
		d := p.Sub(hub)
		if d.Len() < .002 {
			return nil
		}
		ordered[i] = attachment{i, math.Atan2(d.Y, d.X), hub.Add(d.Mul(.70 + .18*random(20+int64(i))))}
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].angle < ordered[j].angle })
	var constraints []webConstraint
	addNode := func(p geom.V) int {
		n.Nodes = append(n.Nodes, p)
		return len(n.Nodes) - 1
	}
	addThread := func(a, b int, kind WebThreadKind) {
		n.Threads = append(n.Threads, WebThread{a, b, kind})
		shrink, stiffness := .98, .7
		switch kind {
		case WebSupport:
			shrink, stiffness = .96, 1
		case WebRadial:
			shrink, stiffness = .94, .45
		case WebCapture:
			shrink, stiffness = .99, .18
		}
		constraints = append(constraints, webConstraint{n.Nodes[b].Sub(n.Nodes[a]).Len() * shrink, stiffness})
	}
	// Each frame junction has its own rock attachment. Intermediate frame
	// nodes are pulled toward the hub, producing scallops between supports.
	for i, a := range ordered {
		b := ordered[(i+1)%len(ordered)]
		gap := b.angle - a.angle
		if i == len(ordered)-1 {
			gap += 2 * math.Pi
		}
		if gap >= math.Pi || gap <= 1e-5 {
			return nil // The attachments do not surround a usable open hub.
		}
		count := max(2, int(math.Ceil(gap/(math.Pi/10))))
		for j := 0; j < count; j++ {
			u := float64(j) / float64(count)
			if j > 0 {
				u += (.32*random(40+int64(len(n.Frame))) - .16) / float64(count)
			}
			p := geom.LerpVector(a.p, b.p, u)
			p = geom.LerpVector(p, hub, .11*math.Sin(math.Pi*u))
			node := addNode(p)
			n.Frame = append(n.Frame, node)
			if j == 0 {
				addThread(a.node, node, WebSupport)
			}
		}
	}
	minReach := math.Inf(1)
	for i, node := range n.Frame {
		addThread(node, n.Frame[(i+1)%len(n.Frame)], WebFrame)
		minReach = min(minReach, n.Nodes[node].Sub(hub).Len())
	}
	if minReach < .004 {
		return nil
	}
	// The inner capture area is compact; successive turns stretch toward the
	// available frame. Every capture knot is also a node on its radial thread.
	turns := max(3, min(11, int(minReach/.0022)))
	knots := make([][]int, turns+1)
	for turn := 0; turn <= turns; turn++ {
		knots[turn] = make([]int, len(n.Frame))
		for spoke, node := range n.Frame {
			d := n.Nodes[node].Sub(hub)
			r := minReach * .075
			if turn > 0 {
				u := float64(turn) / float64(turns)
				phase := math.Atan2(d.Y, d.X)
				u += .012 * math.Sin(phase*2+variation*6+float64(turn)*.7) * math.Sin(math.Pi*u)
				r += minReach*.73*u + (d.Len()*.85-minReach*.73)*u*u
			}
			knots[turn][spoke] = addNode(hub.Add(d.Norm().Mul(r)))
			if turn > 0 {
				addThread(knots[turn-1][spoke], knots[turn][spoke], WebRadial)
			}
		}
	}
	n.Hub = append([]int(nil), knots[0]...)
	for spoke, node := range n.Frame {
		addThread(knots[turns][spoke], node, WebRadial)
		addThread(knots[0][spoke], knots[0][(spoke+1)%len(n.Frame)], WebHub)
	}
	for turn := 1; turn <= turns; turn++ {
		for spoke, a := range knots[turn] {
			b := knots[turn][(spoke+1)%len(n.Frame)]
			// Sparse missing outer sections leave radial support intact.
			if turn > turns/2 && random(1000+int64(turn*len(n.Frame)+spoke)) < .025 {
				continue
			}
			p, q := n.Nodes[a], n.Nodes[b]
			mid := geom.LerpVector(p, q, .5)
			bend := addNode(mid.Add(hub.Sub(mid).Norm().Mul(p.Sub(q).Len() * .035)))
			addThread(a, bend, WebCapture)
			addThread(bend, b, WebCapture)
		}
	}
	relaxWebNetwork(n, constraints)
	if !validWebNetwork(n) {
		return nil
	}
	for i := range n.Nodes {
		if i < n.Anchors {
			n.Nodes[i] = anchors[i]
		} else {
			n.Nodes[i] = n.Nodes[i].Add(origin)
		}
	}
	return n
}

// Silk only resists extension. Fixed anchors and role-specific rest lengths
// determine the resting shape; there is no pressure inflating the perimeter.
func relaxWebNetwork(n *WebNetwork, constraints []webConstraint) {
	for step := 0; step < 40; step++ {
		for i, thread := range n.Threads {
			d := n.Nodes[thread.B].Sub(n.Nodes[thread.A])
			length := d.Len()
			c := constraints[i]
			if length <= c.rest || length < 1e-12 {
				continue
			}
			aFree, bFree := thread.A >= n.Anchors, thread.B >= n.Anchors
			weights := 0.0
			if aFree {
				weights++
			}
			if bFree {
				weights++
			}
			if weights == 0 {
				continue
			}
			correction := d.Mul((length - c.rest) / length * c.stiffness / weights)
			if aFree {
				n.Nodes[thread.A] = n.Nodes[thread.A].Add(correction)
			}
			if bFree {
				n.Nodes[thread.B] = n.Nodes[thread.B].Sub(correction)
			}
		}
	}
}

func validWebNetwork(n *WebNetwork) bool {
	frame := make([]geom.V, len(n.Frame))
	for i, node := range n.Frame {
		frame[i] = n.Nodes[node]
	}
	if geom.PolygonArea(frame) < .000025 {
		return false
	}
	for i, a := range frame {
		b := frame[(i+1)%len(frame)]
		for j := i + 2; j < len(frame); j++ {
			if i == 0 && j == len(frame)-1 {
				continue
			}
			if geom.SegmentDistanceSquared(a, b, frame[j], frame[(j+1)%len(frame)]) < 1e-20 {
				return false
			}
		}
	}
	for _, node := range n.Hub {
		if !geom.InsidePolygon(n.Nodes[node], frame) {
			return false
		}
	}
	for _, t := range n.Threads {
		if n.Nodes[t.A].Sub(n.Nodes[t.B]).Len() < 1e-7 {
			return false
		}
	}
	return true
}
