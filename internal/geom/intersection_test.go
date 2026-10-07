package geom

import (
	"math"
	"testing"
)

func TestQueryCapsuleEndCapsTangencyAndCollinear(t *testing.T) {
	a, b := V{.3, -.8}, V{.5, -.8}
	for _, tc := range []struct {
		origin, direction V
		radius, want      float64
	}{
		{V{.1, -.8}, V{1, 0}, 0, .2},
		{V{.1, -.8}, V{1, 0}, .02, .18},
		{V{.1, -.82}, V{1, 0}, .02, .2},
		{V{.4, -.9}, V{0, 1}, .02, .08},
	} {
		d, n, inside, ok := RayCapsule(tc.origin, tc.direction, a, b, tc.radius, 1)
		if !ok || inside || math.Abs(d-tc.want) > 1e-8 || math.Abs(n.Len()-1) > 1e-9 {
			t.Fatalf("capsule %+v: %v %v %v %v", tc, d, n, inside, ok)
		}
	}
	if _, _, _, ok := RayCapsule(V{.1, -.83}, V{1, 0}, a, b, .02, 1); ok {
		t.Fatal("parallel ray outside guide radius hit")
	}
}
