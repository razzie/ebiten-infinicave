package infinicave

import (
	"image/color"
	"testing"
	"time"
)

func terrainRock(poly []V) RockCell {
	return RockCell{Polygon: poly, Center: faceCenter(poly), Raised: true, Color: color.NRGBA{A: 255}}
}

func terrainRect(x, y, width, height float64) RockCell {
	return terrainRock([]V{{x, y}, {x + width, y}, {x + width, y + height}, {x, y + height}})
}

func TestDiagnosticWorkerRetainsTerrainGeometry(t *testing.T) {
	w := newWorld(42, ViewClay, 0, testLedgeSection)
	defer w.close()
	w.request(0)
	select {
	case mesh := <-w.results:
		if mesh.geometry == nil || len(mesh.geometry.blocks) == 0 || len(mesh.geometry.guides) == 0 || len(mesh.geometry.collision.Polygons) == 0 {
			t.Fatal("diagnostic view lost terrain or collision geometry")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("terrain geometry preparation stalled")
	}
}

func TestTerrainConnectedBlocks(t *testing.T) {
	grid := RockGrid{
		terrainRect(0.03, 0.1, 0.02, 0.04),
		// Two shorter edges share the first cell's long edge.
		terrainRect(0.05, 0.1, 0.03, 0.02),
		terrainRect(0.05, 0.12, 0.03, 0.02),
		// Corner contact and empty space must not join formations.
		terrainRect(0.08, 0.14, 0.02, 0.02),
		terrainRect(0.13, 0.1, 0.03, 0.04),
		// Deep concavity: its empty notch remains unselectable.
		terrainRock([]V{{0.2, 0.1}, {0.24, 0.1}, {0.24, 0.11}, {0.21, 0.11}, {0.21, 0.14}, {0.2, 0.14}}),
	}
	hidden := terrainRect(0.3, 0.1, 0.04, 0.04)
	hidden.Color.A = 0
	grid = append(grid, hidden)
	background := terrainRect(0.35, 0.1, 0.04, 0.04)
	background.Raised = false
	grid = append(grid, background)
	h := prepareTerrainGeometry(sectionData{foreground: grid}, 0)
	if len(h.blocks) != 4 {
		t.Fatalf("got %d blocks, want 4", len(h.blocks))
	}
	scene := &Scene{world: &world{sections: map[int64]*worldSection{0: {geometry: h}}}}
	query := func(p V) QueryResult { return rockAt(t, scene, p.Add(V{Y: -1})) }
	first := query(V{0.04, 0.11}).Hit.FormationID
	for _, p := range []V{{0.06, 0.11}, {0.06, 0.13}, {0.05, 0.12}} {
		if got := query(p); !got.Found || got.Hit.FormationID != first {
			t.Fatalf("connected face at %v did not select the entire block", p)
		}
	}
	for _, p := range []V{{0.09, 0.15}, {0.14, 0.11}, {0.205, 0.13}} {
		if got := query(p); !got.Found || got.Hit.FormationID == first {
			t.Fatalf("separate block at %v selected incorrectly", p)
		}
	}
	for _, p := range []V{{0.12, 0.12}, {0.23, 0.13}, {0.32, 0.12}, {0.37, 0.12}} {
		if got := query(p); got.Found {
			t.Fatalf("empty, transparent, or background point %v selected a block", p)
		}
	}
}
