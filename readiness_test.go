package infinicave

import (
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

func TestReadinessSeparatesForegroundFromDetails(t *testing.T) {
	for _, orientation := range []Orientation{Vertical, Horizontal} {
		scene, err := NewScene(Config{Orientation: orientation})
		if err != nil {
			t.Fatal(err)
		}
		viewport := Viewport{Y: -1, Height: 1}
		if orientation == Horizontal {
			viewport = Viewport{X: 0, Width: 1}
		}
		if got := scene.Readiness(viewport); got != (SceneReadiness{}) {
			t.Fatalf("empty scene ready: %+v", got)
		}
		scene.world.collision = map[int64]*terrain.Geometry{0: {}}
		if got := scene.Readiness(viewport); !got.Collision || got.Foreground {
			t.Fatalf("early collision readiness: %+v", got)
		}
		scene.world.sections[0] = &worldSection{geometry: &terrain.Geometry{}, foreground: ebiten.NewImage(8, 8), pixels: 8, vegetationPending: true}
		scene.world.pixels = 8
		got := scene.Readiness(viewport)
		if !got.Collision || !got.Foreground || got.Background || got.Decorations {
			t.Fatalf("rock-base readiness: %+v", got)
		}
		if len(scene.world.jobs) != 0 {
			t.Fatal("readiness requested generation")
		}
		scene.world.sections[0].terrain = ebiten.NewImage(8, 8)
		scene.world.sections[0].vegetationPending = false
		scene.world.sections[1] = &worldSection{mesh: &render.SectionMesh{ID: 1, Layers: render.AllLayers}}
		if got := scene.Readiness(viewport); !got.Background || !got.Decorations {
			t.Fatalf("finished layers: %+v", got)
		}
		scene.SetRenderWidth(9)
		if got := scene.Readiness(viewport); !got.Collision || got.Foreground || got.Background {
			t.Fatalf("resized readiness: %+v", got)
		}
		scene.Close()
		if got := scene.Readiness(viewport); got != (SceneReadiness{}) {
			t.Fatal("closed scene ready")
		}
	}
}
