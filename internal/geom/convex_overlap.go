package geom

// ConvexOverlap tests positive-area overlap of counterclockwise convex loops.
// It allocates nothing and excludes boundary-only contacts.
func ConvexOverlap(a, b []V) bool {
	if len(a) < 3 || len(b) < 3 {
		return false
	}
	separates := func(poly, other []V) bool {
		for i, p := range poly {
			d := poly[(i+1)%len(poly)].Sub(p)
			inside := false
			for _, q := range other {
				if Cross(d, q.Sub(p)) > 1e-16 {
					inside = true
					break
				}
			}
			if !inside {
				return true
			}
		}
		return false
	}
	return !separates(a, b) && !separates(b, a)
}
