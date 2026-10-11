package render

import (
	"math"
	"sort"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// The orb has a hub in open space, a complete set of spokes around it, and
// closed capture rings. Three frame corners remain attached to the rock.
type webThread struct {
	points         []geom.V
	opacity, width float64
}

func spiderWebThreads(w terrain.SpiderWeb) []webThread {
	hub, radius := w.Orb()
	if radius <= 0 {
		return nil
	}
	var threads []webThread
	frame := w.Frame()
	threads = append(threads, webThread{points: append(append([]geom.V(nil), frame...), frame[0]), opacity: .38, width: .50})
	// Include the three anchor directions, subdividing the remaining angles
	// into roughly thirty-degree intervals. Every spoke meets the frame.
	var anchors []float64
	for _, p := range []geom.V{w.Corner, w.A, w.B} {
		d := p.Sub(hub)
		anchors = append(anchors, math.Atan2(d.Y, d.X))
	}
	sort.Float64s(anchors)
	var angles []float64
	for i, a := range anchors {
		b := anchors[(i+1)%3]
		if i == 2 {
			b += 2 * math.Pi
		}
		count := max(1, int(math.Round((b-a)/(math.Pi/6))))
		for j := 0; j < count; j++ {
			angles = append(angles, geom.Lerp(a, b, float64(j)/float64(count)))
		}
	}
	for _, angle := range angles {
		dir := geom.V{X: math.Cos(angle), Y: math.Sin(angle)}
		reach := math.Inf(1)
		for i, a := range frame {
			edge := frame[(i+1)%len(frame)].Sub(a)
			den := geom.Cross(dir, edge)
			if math.Abs(den) < 1e-12 {
				continue
			}
			delta := a.Sub(hub)
			r := geom.Cross(delta, edge) / den
			t := geom.Cross(delta, dir) / den
			if r > 0 && t >= -1e-9 && t <= 1+1e-9 {
				reach = math.Min(reach, r)
			}
		}
		if !math.IsInf(reach, 1) {
			threads = append(threads, webThread{
				points: []geom.V{hub.Add(dir.Mul(radius * .13)), hub.Add(dir.Mul(reach))}, opacity: .46 + .08*w.Variation, width: .50,
			})
		}
	}
	// Keep the ring spacing readable in small pockets; larger webs carry
	// more capture threads instead of stretching the same sparse pattern.
	rings := max(3, min(7, int(radius*RasterPixelsPerUnit/2)))
	for ring := 1; ring <= rings; ring++ {
		r := radius * float64(ring) / float64(rings)
		var points []geom.V
		for i, angle := range angles {
			next := angles[(i+1)%len(angles)]
			if i == len(angles)-1 {
				next += 2 * math.Pi
			}
			a := hub.Add((geom.V{X: math.Cos(angle), Y: math.Sin(angle)}).Mul(r))
			b := hub.Add((geom.V{X: math.Cos(next), Y: math.Sin(next)}).Mul(r))
			// Curved silk sags slightly between spokes. Each ring closes all
			// the way around the hub instead of terminating on the rock lips.
			mid := hub.Add((geom.V{X: math.Cos((angle + next) / 2), Y: math.Sin((angle + next) / 2)}).Mul(r * .91))
			control := mid.Mul(2).Sub(a.Add(b).Mul(.5))
			for step := 0; step < 4; step++ {
				t := float64(step) / 4
				points = append(points, a.Mul((1-t)*(1-t)).Add(control.Mul(2*t*(1-t))).Add(b.Mul(t*t)))
			}
		}
		points = append(points, points[0])
		threads = append(threads, webThread{points: points, opacity: .40 + .08*w.Variation, width: .40})
	}
	return threads
}

func prepareSpiderWebs(webs []terrain.SpiderWeb, owned bool) TriangleMesh {
	mesh := TriangleMesh{premultiplied: true}
	thread := func(a, b geom.V, opacity, width float64) {
		if owned && !inTerrainBand(a, b) {
			return
		}
		n := b.Sub(a).Perp().Norm().Mul(width / (2 * RasterPixelsPerUnit))
		first := uint32(len(mesh.Vertices))
		for _, p := range [...]geom.V{a.Add(n), b.Add(n), b.Sub(n), a.Sub(n)} {
			alpha := float32(opacity)
			mesh.Vertices = append(mesh.Vertices, ebiten.Vertex{
				DstX: float32(p.X * RasterPixelsPerUnit), DstY: float32((p.Y - terrain.GenerationMinY) * RasterPixelsPerUnit),
				SrcX: .5, SrcY: .5,
				ColorR: .85 * alpha, ColorG: .88 * alpha, ColorB: .83 * alpha, ColorA: alpha,
			})
		}
		mesh.Indices = append(mesh.Indices, first, first+1, first+2, first, first+2, first+3)
	}
	for _, w := range webs {
		for _, silk := range spiderWebThreads(w) {
			for i := 1; i < len(silk.points); i++ {
				thread(silk.points[i-1], silk.points[i], silk.opacity, silk.width)
			}
		}
	}
	return mesh
}

// PrepareOwnedSpiderWebs emits threads for the section's owned rock band.
// FinishSectionMesh clips its image bounds to that band before publication.
func PrepareOwnedSpiderWebs(webs []terrain.SpiderWeb) TriangleMesh {
	return prepareSpiderWebs(webs, true)
}
