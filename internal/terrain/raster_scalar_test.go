package terrain

import (
	"math"
	"math/rand"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func equalRaster(t *testing.T, got, want []float64) {
	t.Helper()
	for i := range got {
		if math.Float64bits(got[i]) != math.Float64bits(want[i]) {
			t.Fatalf("sample %d: got %.17g, want %.17g", i, got[i], want[i])
		}
	}
}

// Compare arithmetic exactly, including spans shorter than a vector, tails,
// zeros, infinities, both projection orders, and both rock clamp modes.
func TestRasterKernelsMatchScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(72))
	for _, size := range []int{0, 1, 2, 3, 4, 7, 8, 9, 15, 17, 35, 127, 239} {
		for trial := 0; trial < 12; trial++ {
			got := make([]float64, size)
			for i := range got {
				got[i] = rng.Float64() * .1
				if i%5 == 0 {
					got[i] = 0
				}
				if i%11 == 0 {
					got[i] = math.Inf(1)
				}
			}
			want := append([]float64(nil), got...)
			c := RockCell{Center: geom.V{X: rng.Float64(), Y: rng.Float64()}, Z: rng.Float64() * .07,
				Normal: geom.V3{X: rng.Float64() - .5, Y: rng.Float64() - .5, Z: rng.Float64()}, Raised: trial%2 == 0}
			py := rng.Float64()*3 - 1
			rasterDepthSpan(got, 7, py, c)
			rasterDepthSpanScalar(want, 7, py, c)
			equalRaster(t, got, want)
			a, d := geom.V{X: rng.Float64(), Y: py - .02}, geom.V{X: .025, Y: .04}
			for _, divide := range []bool{false, true} {
				for _, clearance := range []bool{false, true} {
					scale := d.Len2()
					if !divide {
						scale = 1 / scale
					}
					segmentDistanceSpan(got, 7, py, a, d, scale, divide, clearance)
					segmentDistanceSpanScalar(want, 7, py, a, d, scale, divide, clearance)
					equalRaster(t, got, want)
				}
			}
			// Tone buffers are finite; distance buffers can contain infinity.
			fillFloat64(got, 36)
			fillFloat64Scalar(want, 36)
			blendVineTone(got, 57, .7)
			blendVineToneScalar(want, 57, .7)
			equalRaster(t, got, want)
		}
	}
}

func TestVineClassificationMatchesScalar(t *testing.T) {
	for _, foreground := range []bool{false, true} {
		makeField := func() *VineTerrain {
			return &VineTerrain{onForeground: foreground, clearance: make([]float64, vineFieldWidth*vineFieldHeight), unsupported: make([]float64, vineFieldWidth*vineFieldHeight)}
		}
		got, want := makeField(), makeField()
		tones := make([]float64, len(got.clearance))
		values := []float64{0, vineVoidTone, math.Nextafter(vineVoidTone, 100), 36, math.Nextafter(vineLightTone, 0), vineLightTone, math.Inf(1)}
		for i := range tones {
			tones[i] = values[i%len(values)]
		}
		classifyVineTones(got, tones)
		classifyVineTonesScalar(want, tones)
		equalRaster(t, got.clearance, want.clearance)
		equalRaster(t, got.unsupported, want.unsupported)
	}
}
