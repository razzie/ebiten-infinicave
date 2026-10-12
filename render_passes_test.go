package infinicave

import (
	"math"
	"reflect"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
)

func TestRenderPassCoordinatesAndLifecycle(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		scene, err := NewScene(Config{Orientation: orientation})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(scene.Close)
		for _, size := range [][2]int{{100, 180}, {180, 100}, {501, 800}} {
			dst := ebiten.NewImage(size[0], size[1])
			pixels := float64(min(size[0], size[1]))
			viewport := Viewport{Y: -2.003, Height: float64(size[1]) / pixels}
			if orientation == Horizontal {
				viewport = Viewport{X: 2.003, Width: float64(size[0]) / pixels}
			}
			var calls []string
			check := func(name string) RenderPass {
				return func(c RenderContext) {
					calls = append(calls, name)
					if c.Orientation != orientation {
						t.Fatal("pass lost orientation")
					}
					var point V
					var expectedX, expectedY float64
					if orientation == Horizontal {
						point = V{X: c.Viewport.X + .4, Y: .3}
						expectedX, expectedY = .4*pixels, float64(size[1]-int(pixels))/2+.3*pixels
					} else {
						point = V{X: .3, Y: c.Viewport.Y + .4}
						expectedX, expectedY = float64(size[0]-int(pixels))/2+.3*pixels, .4*pixels
					}
					x, y := c.WorldToTarget.Apply(point.X, point.Y)
					if orientation == Horizontal {
						x, y = float64(size[0])-y, x
					}
					if math.Abs(x-expectedX) > 1e-8 || math.Abs(y-expectedY) > 1e-8 {
						t.Fatalf("%s %v: transformed point %g,%g, want %g,%g", orientation, size, x, y, expectedX, expectedY)
					}
					if point.X < c.Min.X || point.X > c.Max.X || point.Y < c.Min.Y || point.Y > c.Max.Y {
						t.Fatal("visible world point excluded from context bounds")
					}
					if orientation == Vertical && c.Dst != dst {
						t.Fatal("vertical pass lost target")
					}
					if orientation == Horizontal && c.Dst == dst {
						t.Fatal("horizontal pass bypassed compositor")
					}
				}
			}
			passes := RenderPasses{BehindRocks: check("back"), AboveRocks: check("front")}
			scene.DrawWithPasses(dst, viewport, passes)
			scene.DrawWithPasses(dst, viewport, passes)
			if !reflect.DeepEqual(calls, []string{"back", "front", "back", "front"}) {
				t.Fatalf("passes must run once in order even while unloaded: %v", calls)
			}
			scene.DrawWithPasses(dst, Viewport{}, passes)
			if len(calls) != 4 {
				t.Fatal("invalid viewport invoked passes")
			}
			dst.Deallocate()
		}
		scene.Close()
		dst := ebiten.NewImage(10, 10)
		scene.DrawWithPasses(dst, Viewport{Y: -1, Height: 1, X: 0, Width: 1}, RenderPasses{
			BehindRocks: func(RenderContext) { t.Fatal("closed scene invoked pass") },
			AboveRocks:  func(RenderContext) { t.Fatal("closed scene invoked pass") },
		})
		dst.Deallocate()
	}
}
