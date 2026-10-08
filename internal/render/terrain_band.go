package render

import (
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Terrain images contain [0, SectionHeight] along the scrolling axis. Cull
// whole primitives only, retaining a small margin for float32 conversion and
// the reference-scale outline width. Ebitengine antialiasing supersamples the
// destination's own bounds; off-image primitives cannot contribute at any
// render width, including one pixel. Lighting and topology keep their padding.
const terrainBandMargin = .002

func inTerrainBand(points ...geom.V) bool {
	below, above := true, true
	for _, p := range points {
		below = below && p.Y < -terrainBandMargin
		above = above && p.Y > terrain.SectionHeight+terrainBandMargin
	}
	return len(points) > 0 && !below && !above
}

// PrepareOwnedGridWithTopology emits terrain for a section's image while
// retaining complete padded topology for shading, collision, and later edits.
func PrepareOwnedGridWithTopology(grid terrain.RockGrid, view View, topology *terrain.RockTopology) GridMesh {
	return prepareGridBand(grid, view, topology, nil, true)
}
