package terrain

import (
	"math"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

var depthSampleX = func() (x [rockDepthWidth]float64) {
	for i := range x {
		x[i] = (float64(i) + .5) * rockDepthStep
	}
	return
}()
var vineSampleX = func() (x [vineFieldWidth]float64) {
	for i := range x {
		x[i] = (float64(i) + .5) * vineFieldStep
	}
	return
}()
var vineSideClearance = func() (x [vineFieldWidth]float64) {
	for i := range x {
		x[i] = float64(min(i+1, vineFieldWidth-i))
	}
	return
}()

func fillFloat64Scalar(dst []float64, value float64) {
	for i := range dst {
		dst[i] = value
	}
}

func rasterDepthSpanScalar(dst []float64, first int, py float64, c RockCell) {
	for i := range dst {
		dst[i] = math.Max(dst[i], RockDepthAt(c, geom.V{X: depthSampleX[first+i], Y: py}))
	}
}

// Border seeding historically multiplies by the inverse length, while guide
// clearance divides by the length. Keep both arithmetic orders exactly.
func segmentDistanceSpanScalar(dst []float64, first int, py float64, a, d geom.V, scale float64, divide, clearance bool) {
	const reach = foregroundVineGuideClearance + vineBorderRange
	for i := range dst {
		if clearance && dst[i] == 0 {
			continue
		}
		p := geom.V{X: vineSampleX[first+i], Y: py}
		t := p.Sub(a).Dot(d)
		if divide {
			t /= scale
		} else {
			t *= scale
		}
		t = geom.Clamp(t, 0, 1)
		distance := p.Sub(a.Add(d.Mul(t))).Len()
		if clearance {
			if distance < reach {
				dst[i] = min(dst[i], max(0, distance-foregroundVineGuideClearance))
			}
		} else {
			dst[i] = min(dst[i], distance)
		}
	}
}

func blendVineToneScalar(dst []float64, tone, alpha float64) {
	for i := range dst {
		dst[i] = geom.Lerp(dst[i], tone, alpha)
	}
}

func classifyVineTonesScalar(f *VineTerrain, tones []float64) {
	for y := 0; y < vineFieldHeight; y++ {
		for x := 0; x < vineFieldWidth; x++ {
			i := y*vineFieldWidth + x
			if tones[i] > vineVoidTone && tones[i] < vineLightTone {
				f.clearance[i] = min(vineSideClearance[x], float64(min(y+1, vineFieldHeight-y))) * vineFieldStep
			} else if !f.onForeground {
				f.unsupported[i] = math.Inf(1)
			}
		}
	}
}
