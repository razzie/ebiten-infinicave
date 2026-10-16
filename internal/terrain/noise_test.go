package terrain

import (
	"math"
	"math/rand"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestNoiseBatchAndFBMMatchScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(93))
	n := NewPerlin(rng)
	n.OffsetY = -71.25
	xs, ys := make([]float64, 67), make([]float64, 67)
	for i := range xs {
		xs[i], ys[i] = rng.Float64()*1000-500, rng.Float64()*1000-500
	}
	copy(xs, []float64{0, math.Copysign(0, -1), 1, -1, math.Nextafter(1, 0), math.Nextafter(-1, 0)})
	copy(ys, xs[:6])
	for count := 0; count <= len(xs); count++ {
		dst := make([]float64, count)
		noiseBatch(n, xs[:count], ys[:count], dst)
		for i, got := range dst {
			if want := n.Noise(xs[i], ys[i]); math.Float64bits(got) != math.Float64bits(want) {
				t.Fatalf("batch %d lane %d: %x != %x", count, i, math.Float64bits(got), math.Float64bits(want))
			}
		}
	}
	for i := range xs {
		p := geom.V{X: xs[i], Y: ys[i]}
		q := p
		q.Y += n.OffsetY
		a, f, sum, norm := 1.0, float64(noiseScale), 0.0, 0.0
		for range 4 {
			sum += a * n.Noise(q.X*f, q.Y*f)
			norm += a
			a *= .52
			f *= 2.08
		}
		if got, want := fbm(n, p), .5+.5*sum/norm; math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("FBM at %v: %x != %x", p, math.Float64bits(got), math.Float64bits(want))
		}
	}
}
