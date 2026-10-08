//go:build !goexperiment.simd

package terrain

import "github.com/razzie/ebiten-infinicave/internal/geom"

func fbm(n *Perlin, p geom.V) float64 { return fbmScalar(n, p) }

func noiseBatch(n *Perlin, xs, ys, dst []float64) { noiseBatchScalar(n, xs, ys, dst) }
