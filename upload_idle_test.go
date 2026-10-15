package infinicave

import (
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
)

func TestPartialRecipeDoesNotRefreshUnavailableSibling(t *testing.T) {
	mesh := render.SectionMesh{Layers: render.BackgroundVinesLayer}
	s := &worldSection{mesh: &mesh, terrain: ebiten.NewImage(16, 16), backgroundPixels: 16, vegetationPending: true}
	s.plants[backgroundVinesProduct].pixels = 32
	mesh.PlantRecipes[backgroundVinesProduct] = new(uint8)
	s.plants[backgroundVinesProduct].recipe = mesh.PlantRecipes[backgroundVinesProduct]
	t.Cleanup(s.deallocate)
	if s.needsRefresh(32, render.AllLayers) {
		t.Fatal("partial vine recipe attempted to resize an unavailable background recipe")
	}
}

func TestEmptyCachedRefreshRunsOncePerUpdate(t *testing.T) {
	for _, test := range []struct {
		name           string
		recipeIdentity bool
	}{{"empty legacy publication", false}, {"stale aggregate", true}} {
		t.Run(test.name, func(t *testing.T) {
			checkEmptyCachedRefresh(t, test.recipeIdentity)
		})
	}
}

func checkEmptyCachedRefresh(t *testing.T, recipeIdentity bool) {
	t.Helper()
	w := requestTestWorld(t)
	w.pixels, w.viewport = 32, Viewport{Y: -.8, Height: .8}
	mesh := render.SectionMesh{Layers: render.BackgroundVinesLayer}
	if recipeIdentity {
		mesh.PlantRecipes[backgroundVinesProduct] = new(uint8)
	}
	s := &worldSection{mesh: &mesh, vegetationPixels: 16, vegetationPending: !recipeIdentity}
	s.plants[backgroundVinesProduct] = plantPublication{pixels: 32, recipe: mesh.PlantRecipes[backgroundVinesProduct]}
	w.sections[0] = s
	// A stale aggregate requests a refresh despite matching product metadata.
	// A legacy partial recipe republishes an empty product but cannot complete
	// aggregate readiness until its siblings arrive. Neither uses draw budget.
	if !w.needsVisibleRefresh(0, s) {
		t.Fatal("fixture does not request a metadata-only refresh")
	}
	g := &Scene{world: w}
	for range 2 {
		before := w.uploadTurn
		budget := uploadBudget{start: time.Now().Add(time.Hour)}
		w.receiveBudget(g, &budget)
		wantTurns := uint64(1)
		if w.uploadTurn-before != wantTurns || w.upload != nil || len(w.parked) != 0 || budget.draws != 0 {
			t.Fatal("a no-work refresh repeated in the same update or held the uploader")
		}
	}
}
