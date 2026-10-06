package infinicave

// Neighboring sections need a refresh only when their retained vegetation
// actually crosses into the viewport. Terrain contributes its owned band.
func meshVisible(mesh sectionMesh, viewport Viewport) bool {
	if !viewport.valid() {
		return true
	}
	bottom := viewport.Y + viewport.Height
	top := sectionTop(mesh.id)
	if top < bottom && top+SectionHeight > viewport.Y {
		return true
	}
	top = sectionWindowTop(mesh.id)
	for _, bounds := range []struct{ min, max int }{
		{mesh.vinesBounds.Min.Y, mesh.vinesBounds.Max.Y},
		{mesh.foregroundVinesBounds.Min.Y, mesh.foregroundVinesBounds.Max.Y},
		{mesh.mushroomsBounds.Min.Y, mesh.mushroomsBounds.Max.Y},
	} {
		if bounds.max > bounds.min && top+float64(bounds.min)/rasterPixelsPerUnit < bottom &&
			top+float64(bounds.max)/rasterPixelsPerUnit > viewport.Y {
			return true
		}
	}
	return false
}

func (w *world) savePending(data sectionMesh) {
	if w.pending == nil {
		w.pending = make(map[int64]sectionMesh)
	}
	w.pending[data.id] = data
}

// Abandon partial GPU work, retaining CPU meshes and already published images.
// Rapid resize events therefore coalesce into work at the latest resolution.
func (w *world) deferUpload() {
	u := w.upload
	if u == nil {
		return
	}
	w.savePending(u.data)
	if u.img != nil {
		u.img.Deallocate()
	}
	if u.foreground != nil {
		u.foreground.Deallocate()
	}
	if u.vines != nil {
		u.vines.Deallocate()
	}
	if u.foregroundVines != nil {
		u.foregroundVines.Deallocate()
	}
	if u.mushrooms != nil {
		u.mushrooms.Deallocate()
	}
	if section := w.sections[u.data.id]; section != nil {
		data := u.data
		section.mesh = &data
	}
	w.upload = nil
}

func (w *world) startUpload(data sectionMesh) {
	w.upload = &sectionUpload{data: data, pixels: w.renderWidth()}
	delete(w.pending, data.id)
}
