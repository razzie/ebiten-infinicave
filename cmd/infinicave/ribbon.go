package main

import (
	"fmt"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave"
)

type ribbonMode uint8

const (
	ribbonOff ribbonMode = iota
	ribbonBack
	ribbonFront
)

func parseRibbonMode(value string) (ribbonMode, error) {
	switch value {
	case "off":
		return ribbonOff, nil
	case "back":
		return ribbonBack, nil
	case "front":
		return ribbonFront, nil
	default:
		return ribbonOff, fmt.Errorf("unknown ribbon mode %q: use off, back, or front", value)
	}
}

const ribbonLifetime = .9 // Seconds; animation advances only in Update.
const ribbonMaxPoints = 128

type ribbonPoint struct {
	position infinicave.V
	age      float64
}

type ribbonTrail struct {
	mode     ribbonMode
	points   []ribbonPoint
	white    *ebiten.Image
	vertices []ebiten.Vertex
	indices  []uint32
}

func newRibbonTrail(mode ribbonMode) *ribbonTrail {
	r := &ribbonTrail{mode: mode, white: ebiten.NewImage(1, 1)}
	r.white.Fill(color.White)
	return r
}

func (r *ribbonTrail) close() { r.white.Deallocate() }

func (r *ribbonTrail) step(cursor infinicave.V, inside bool, dt float64) {
	first := 0
	for i := range r.points {
		r.points[i].age += dt
		if r.points[i].age >= ribbonLifetime {
			first = i + 1
		}
	}
	r.points = r.points[:copy(r.points, r.points[first:])]
	if !inside {
		return
	}
	if len(r.points) > 0 && cursor.Sub(r.points[len(r.points)-1].position).Len() < .0005 {
		// Keep the head on the cursor while older samples fade away.
		r.points[len(r.points)-1].position = cursor
		r.points[len(r.points)-1].age = 0
		return
	}
	if len(r.points) == ribbonMaxPoints {
		copy(r.points, r.points[1:])
		r.points = r.points[:ribbonMaxPoints-1]
	}
	r.points = append(r.points, ribbonPoint{position: cursor})
}

func (r *ribbonTrail) passes() infinicave.RenderPasses {
	if r.mode == ribbonBack {
		return infinicave.RenderPasses{BehindRocks: r.draw}
	}
	if r.mode == ribbonFront {
		return infinicave.RenderPasses{AboveRocks: r.draw}
	}
	return infinicave.RenderPasses{}
}

func (r *ribbonTrail) draw(context infinicave.RenderContext) {
	if len(r.points) < 2 {
		return
	}
	r.vertices, r.indices = r.vertices[:0], r.indices[:0]
	for i, point := range r.points {
		previous := r.points[max(0, i-1)].position
		next := r.points[min(len(r.points)-1, i+1)].position
		tangent := next.Sub(previous)
		if tangent.Len() < 1e-9 {
			tangent = point.position.Sub(previous)
		}
		life := max(0, 1-point.age/ribbonLifetime)
		width := .013 * life
		// A short taper closes the oldest end of the ribbon smoothly.
		width *= min(1, float64(i)/3)
		normal := tangent.Norm().Perp().Mul(width)
		for _, position := range []infinicave.V{point.position.Add(normal), point.position.Sub(normal)} {
			x, y := context.WorldToTarget.Apply(position.X, position.Y)
			r.vertices = append(r.vertices, ebiten.Vertex{
				DstX: float32(x), DstY: float32(y), SrcX: .5, SrcY: .5,
				ColorR: float32(.25 + .65*(1-life)), ColorG: float32(.35 + .6*life),
				ColorB: 1, ColorA: float32(.9 * life),
			})
		}
		if i > 0 {
			v := uint32(i * 2)
			r.indices = append(r.indices, v-2, v-1, v+1, v-2, v+1, v)
		}
	}
	context.Dst.DrawTriangles32(r.vertices, r.indices, r.white, &ebiten.DrawTrianglesOptions{AntiAlias: context.Antialiasing.GeometryAA()})
}

func (g *Game) updateRibbon() {
	if g.ribbon == nil {
		return
	}
	x, y := ebiten.CursorPosition()
	inside := x >= 0 && y >= 0 && x < g.screenWidth && y < g.screenHeight
	g.ribbon.step(g.worldPoint(infinicave.V{X: float64(x), Y: float64(y)}), inside, 1/float64(ebiten.TPS()))
}
