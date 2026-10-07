package geom

import (
	"math"
)

// Screen Y increases downward; positive Z points toward the camera.
type V3 struct{ X, Y, Z float64 }

func (v V3) Norm() V3 {
	length := math.Sqrt(v.X*v.X + v.Y*v.Y + v.Z*v.Z)
	if length == 0 {
		return V3{Z: 1}
	}
	return V3{v.X / length, v.Y / length, v.Z / length}
}

func Rotate(v V, angle float64) V {
	c, s := math.Cos(angle), math.Sin(angle)
	return V{v.X*c - v.Y*s, v.X*s + v.Y*c}
}

type V struct{ X, Y float64 }

func (a V) Add(b V) V { return V{a.X + b.X, a.Y + b.Y} }

func (a V) Sub(b V) V { return V{a.X - b.X, a.Y - b.Y} }

func (a V) Mul(s float64) V { return V{a.X * s, a.Y * s} }

func (a V) Dot(b V) float64 { return a.X*b.X + a.Y*b.Y }

func (a V) Len2() float64 { return a.Dot(a) }

func (a V) Len() float64 { return math.Sqrt(a.Len2()) }

func (a V) Perp() V { return V{-a.Y, a.X} }

func LerpVector(a, b V, t float64) V { return a.Mul(1 - t).Add(b.Mul(t)) }

func Clamp(x, a, b float64) float64 { return max(a, min(b, x)) }

func Lerp(a, b, t float64) float64 { return a + (b-a)*t }

func (a V) Norm() V {
	l := a.Len()
	if l == 0 {
		return V{1, 0}
	}
	return a.Mul(1 / l)
}

func Smoothstep(a, b, x float64) float64 {
	t := Clamp((x-a)/(b-a), 0, 1)
	return t * t * (3 - 2*t)
}
