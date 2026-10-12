package terrain

import (
	"context"
	"image/color"
	"math/rand"
	"testing"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func TestForegroundFamiliesKeepFineAttachedBranches(t *testing.T) {
	rock := testRockGrid([]geom.V{{X: .5, Y: .5}}, []color.NRGBA{{30, 30, 30, 255}})
	for _, foreground := range []bool{false, true} {
		field := newVineTerrain(rock, nil)
		if foreground {
			field.release()
			field = newForegroundVineTerrain(rock, nil)
		}
		vines := generateVinesInBandContext(context.Background(), field, rand.New(rand.NewSource(42)), 0, SectionHeight, 1)
		field.release()
		main, secondary := 0, 0
		maxRadius := vineMaxRadius
		if foreground {
			maxRadius = .0015
		}
		for i, vine := range vines {
			for _, p := range vine.Points {
				if p.Radius > maxRadius {
					t.Fatal("fine vine family grew a broad stem")
				}
				if foreground && vine.Depth > 0 && p.Radius > maxRadius*.55 {
					t.Fatal("fine branches did not taper below their trunk width")
				}
			}
			if vine.EdgeAligned || vine.Parent < 0 {
				continue
			}
			if vine.Depth == 1 {
				main++
			} else {
				secondary++
			}
			if vine.Parent >= i || vine.Points[0].P != vines[vine.Parent].Points[vine.Joint].P {
				t.Fatal("branch detached from its attachment")
			}
		}
		if foreground {
			if main < 2 || main > 4 || secondary > main {
				t.Fatalf("foreground needs a small attached family: main=%d secondary=%d", main, secondary)
			}
		} else if main < 1 || main > 3 || secondary > 2 {
			t.Fatalf("background kept dense branching: main=%d secondary=%d", main, secondary)
		}
	}
}
