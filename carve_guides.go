package infinicave

import (
	"maps"

	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Complete formations nominate their source guides for retirement. A guide
// shared by another formation, split fragment, or padded continuation stays
// alive until the last supporting shape disappears.
func (w *world) retireGuides(candidates map[[32]byte]bool) {
	if len(candidates) == 0 {
		return
	}
	keep := func(h *terrain.Geometry) {
		if h == nil {
			return
		}
		for face := range h.Faces {
			for _, key := range h.FaceGuideKeys(face) {
				delete(candidates, key)
			}
		}
	}
	for _, section := range w.sections {
		keep(section.geometry)
	}
	for _, h := range w.collision {
		keep(h)
	}
	if len(candidates) == 0 {
		return
	}
	// Jobs retain the old map, so publish a new snapshot instead of mutating
	// data that a generation worker may currently be reading.
	retired := maps.Clone(w.retiredGuides)
	if retired == nil {
		retired = make(map[[32]byte]bool)
	}
	maps.Copy(retired, candidates)
	w.retiredGuides = retired
	for _, section := range w.sections {
		section.geometry = section.geometry.WithoutGuides(retired)
		if section.mesh != nil {
			mesh := *section.mesh
			mesh.Geometry = mesh.Geometry.WithoutGuides(retired)
			section.mesh = &mesh
		}
	}
	for id, h := range w.collision {
		w.collision[id] = h.WithoutGuides(retired)
	}
	for id, mesh := range w.pending {
		mesh.Geometry = mesh.Geometry.WithoutGuides(retired)
		w.pending[id] = mesh
	}
	if w.upload != nil {
		w.upload.data.Geometry = w.upload.data.Geometry.WithoutGuides(retired)
		w.upload.raster.Geometry = w.upload.raster.Geometry.WithoutGuides(retired)
	}
	for _, upload := range w.parked {
		upload.data.Geometry = upload.data.Geometry.WithoutGuides(retired)
		upload.raster.Geometry = upload.raster.Geometry.WithoutGuides(retired)
	}
	w.revision++
	w.queryIndex() // release guide point copies and old formation source snapshots
}
