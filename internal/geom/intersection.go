package geom

import (
	"math"
)

const QueryEpsilon = 1e-10

func CopyPolygons(polygons [][]V) [][]V {
	copy := make([][]V, len(polygons))
	for i, poly := range polygons {
		copy[i] = append([]V(nil), poly...)
	}
	return copy
}

func RayBox(origin, direction V, distance float64, lo, hi V) (float64, float64, bool) {
	start, end := 0.0, distance
	for _, axis := range [][4]float64{{origin.X, direction.X, lo.X, hi.X}, {origin.Y, direction.Y, lo.Y, hi.Y}} {
		if axis[1] == 0 {
			if axis[0] < axis[2] || axis[0] > axis[3] {
				return 0, 0, false
			}
			continue
		}
		a, b := (axis[2]-axis[0])/axis[1], (axis[3]-axis[0])/axis[1]
		if a > b {
			a, b = b, a
		}
		start, end = math.Max(start, a), math.Min(end, b)
		if start > end {
			return 0, 0, false
		}
	}
	return start, end, true
}

func RaySegment(origin, direction, a, b V, limit float64) (float64, bool) {
	edge, offset := b.Sub(a), a.Sub(origin)
	denominator := Cross(direction, edge)
	if math.Abs(denominator) <= 1e-14*math.Max(edge.Len(), 1e-12) {
		if math.Abs(Cross(offset, direction)) > QueryEpsilon {
			return 0, false
		}
		t0, t1 := offset.Dot(direction), b.Sub(origin).Dot(direction)
		start, end := math.Min(t0, t1), math.Max(t0, t1)
		if end < -QueryEpsilon || start > limit+QueryEpsilon {
			return 0, false
		}
		return math.Max(0, start), true
	}
	distance, fraction := Cross(offset, edge)/denominator, Cross(offset, direction)/denominator
	if distance < -QueryEpsilon || distance > limit+QueryEpsilon || fraction < -QueryEpsilon || fraction > 1+QueryEpsilon {
		return 0, false
	}
	return Clamp(distance, 0, limit), true
}

// A guide's selectable area is the union of segment capsules, including round
// end caps. This computes the first entry, not merely the closest approach.
func RayCapsule(origin, direction, a, b V, radius, limit float64) (float64, V, bool, bool) {
	if SegmentDistanceSquared(origin, origin, a, b) <= radius*radius+1e-20 {
		return 0, V{}, true, true
	}
	if direction == (V{}) {
		return 0, V{}, false, false
	}
	if radius == 0 {
		distance, ok := RaySegment(origin, direction, a, b, limit)
		normal := b.Sub(a).Perp().Norm()
		if normal.Dot(direction) > 0 {
			normal = normal.Mul(-1)
		}
		return distance, normal, false, ok
	}
	best, normal := math.Inf(1), V{}
	consider := func(distance float64, n V) {
		if distance >= -QueryEpsilon && distance <= limit+QueryEpsilon && distance < best {
			best, normal = math.Max(0, distance), n
		}
	}
	edge := b.Sub(a)
	if length := edge.Len(); length > 0 {
		tangent := edge.Mul(1 / length)
		perpendicular := tangent.Perp()
		if speed := direction.Dot(perpendicular); speed != 0 {
			for _, sign := range []float64{-1, 1} {
				distance := (sign*radius - origin.Sub(a).Dot(perpendicular)) / speed
				along := origin.Add(direction.Mul(distance)).Sub(a).Dot(tangent)
				if along >= 0 && along <= length {
					consider(distance, perpendicular.Mul(sign))
				}
			}
		}
	}
	for _, center := range []V{a, b} {
		offset := center.Sub(origin)
		projection := offset.Dot(direction)
		perpendicular := Cross(offset, direction)
		discriminant := radius*radius - perpendicular*perpendicular
		if discriminant >= 0 {
			distance := projection - math.Sqrt(discriminant)
			n := origin.Add(direction.Mul(distance)).Sub(center).Norm()
			consider(distance, n)
		}
	}
	return best, normal, false, !math.IsInf(best, 1)
}

// Segment distances also catch crossings between vertices and close parallel
// edges; checking only knots or bounding boxes misses both.
func SegmentDistanceSquared(a, b, c, d V) float64 {
	ab, cd := b.Sub(a), d.Sub(c)
	den := Cross(ab, cd)
	if math.Abs(den) > 1e-15 {
		u, v := Cross(c.Sub(a), cd)/den, Cross(c.Sub(a), ab)/den
		if u >= 0 && u <= 1 && v >= 0 && v <= 1 {
			return 0
		}
	}
	pointDistance := func(p, q, r V) float64 {
		delta := r.Sub(q)
		u := 0.0
		if delta.Len2() > 0 {
			u = Clamp(p.Sub(q).Dot(delta)/delta.Len2(), 0, 1)
		}
		return p.Sub(q.Add(delta.Mul(u))).Len2()
	}
	return min(pointDistance(a, c, d), pointDistance(b, c, d), pointDistance(c, a, b), pointDistance(d, a, b))
}
