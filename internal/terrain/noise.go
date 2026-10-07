package terrain

import (
	"math"
	"math/rand"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// Classic gradient Perlin noise.
type Perlin struct {
	p       [512]int
	OffsetY float64
}

func NewPerlin(rng *rand.Rand) *Perlin {
	q := rng.Perm(256)
	n := &Perlin{}
	for i := 0; i < 512; i++ {
		n.p[i] = q[i&255]
	}
	return n
}

func fade(t float64) float64 { return t * t * t * (t*(t*6-15) + 10) }

func grad(h int, x, y float64) float64 {
	switch h & 7 {
	case 0:
		return x + y
	case 1:
		return -x + y
	case 2:
		return x - y
	case 3:
		return -x - y
	case 4:
		return x
	case 5:
		return -x
	case 6:
		return y
	default:
		return -y
	}
}

func (n *Perlin) Noise(x, y float64) float64 {
	x0 := math.Floor(x)
	y0 := math.Floor(y)
	xi := int(x0) & 255
	yi := int(y0) & 255
	xf := x - x0
	yf := y - y0
	u, v := fade(xf), fade(yf)

	aa := n.p[n.p[xi]+yi]
	ab := n.p[n.p[xi]+yi+1]
	ba := n.p[n.p[xi+1]+yi]
	bb := n.p[n.p[xi+1]+yi+1]

	x1 := geom.Lerp(grad(aa, xf, yf), grad(ba, xf-1, yf), u)
	x2 := geom.Lerp(grad(ab, xf, yf-1), grad(bb, xf-1, yf-1), u)
	return geom.Lerp(x1, x2, v) * 0.7071
}

func fbm(n *Perlin, p geom.V) float64 {
	p.Y += n.OffsetY
	a, f, sum, norm := 1.0, noiseScale, 0.0, 0.0
	for i := 0; i < 4; i++ {
		sum += a * n.Noise(p.X*f, p.Y*f)
		norm += a
		a *= 0.52
		f *= 2.08
	}
	return 0.5 + 0.5*sum/norm
}
