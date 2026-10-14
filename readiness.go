package infinicave

// SceneReadiness reports independently drawable layers for a viewport.
// Disabled layers count as ready. It does not request or wait for generation.
type SceneReadiness struct {
	Collision, Foreground, Background, Decorations bool
}

// Readiness reports current layer availability. Call on the game goroutine.
// Foreground becomes ready before background and decorations. Update continues
// to return whether all layers needed by the viewport have finished.
func (g *Scene) Readiness(viewport Viewport) SceneReadiness {
	viewport, valid := orientedViewport(g.orientation, viewport)
	if g.closed || !valid || g.world == nil {
		return SceneReadiness{}
	}
	w := g.world
	ready := SceneReadiness{Collision: true, Foreground: true, Background: true, Decorations: true}
	low, high := visibleSections(viewport.Y, viewport.Height)
	for id := low; id <= high; id++ {
		s := w.sections[id]
		if s == nil || s.geometry == nil {
			ready.Collision = ready.Collision && w.collision[id] != nil
		}
		if s == nil || s.foreground == nil || s.renderWidth() != w.renderWidth() {
			ready.Foreground = false
		}
		if !g.disableBackground && (s == nil || s.terrain == nil || s.backgroundWidth() != w.renderWidth()) {
			ready.Background = false
		}
	}
	// Until ownership migrates to shared references, neighboring padded recipes
	// may supply plants to this viewport. Unknown recipes are still pending.
	for id := max(0, low-1); id <= high+1; id++ {
		s := w.sections[id]
		if s != nil && s.mesh != nil && !s.mesh.TerrainOnly && !meshVisible(*s.mesh, viewport) {
			continue
		}
		if s == nil || s.vegetationPending || s.vegetationWidth() != w.renderWidth() {
			ready.Decorations = false
		}
	}
	return ready
}
