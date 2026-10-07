package terrain

import (
	"math"
	"runtime"
	"testing"
)

func TestChamferFieldMatchesSingleObstacleDistances(t *testing.T) {
	d := takeVineField()
	for i := range d {
		d[i] = math.Inf(1)
	}
	const cx, cy = 173, 611
	d[cy*vineFieldWidth+cx] = 0
	chamferVineField(d)
	for y := 0; y < vineFieldHeight; y++ {
		for x := 0; x < vineFieldWidth; x++ {
			dx, dy := abs(x-cx), abs(y-cy)
			want := float64(max(dx, dy)-min(dx, dy))*vineFieldStep + float64(min(dx, dy))*vineFieldStep*math.Sqrt2
			if math.Abs(d[y*vineFieldWidth+x]-want) > 1e-10 {
				t.Fatalf("distance at (%d,%d) = %g, want %g", x, y, d[y*vineFieldWidth+x], want)
			}
		}
	}
	putVineField(d)
	d = takeVineField()
	defer putVineField(d)
	for _, value := range d {
		if value != 0 {
			t.Fatal("pooled distance buffer contains old terrain")
		}
	}
}

func TestVineWorkspaceSurvivesGCWithoutLeakingOldSamples(t *testing.T) {
	var workspace vineWorkspace
	field := workspace.take()
	field[0], field[len(field)-1] = 123, 456
	address := &field[0]
	workspace.put(field)
	runtime.GC()
	reused := workspace.take()
	if &reused[0] != address || reused[0] != 0 || reused[len(reused)-1] != 0 {
		t.Fatal("worker workspace lost its buffer or retained the previous section")
	}
}
