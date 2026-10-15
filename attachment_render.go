package infinicave

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/render"
)

func (w *world) prepareAtlas(kind render.AtlasKind, budget *uploadBudget) bool {
	if kind == render.NoAtlas {
		return true
	}
	if w.atlases == nil {
		w.atlases = &render.AtlasCache{Antialiasing: w.antialiasing}
	}
	return w.atlases.Prepare(kind, func(indices int) bool { return budget.take(indices) })
}

func (g *Scene) drawAttachmentMesh(dst *ebiten.Image, mesh render.TriangleMesh) {
	if dst == nil || len(mesh.Indices) == 0 {
		return
	}
	if mesh.Texture == render.NoAtlas {
		mesh.DrawWithAntialiasing(dst, g.world.white, g.antialiasing.GeometryAA())
		return
	}
	if g.world.atlases == nil {
		g.world.atlases = &render.AtlasCache{Antialiasing: g.antialiasing}
	}
	g.world.atlases.Prepare(mesh.Texture, func(int) bool { return true })
	mesh.DrawWithAntialiasing(dst, g.world.atlases.Image(mesh.Texture), g.antialiasing.GeometryAA())
}

func (w *world) uploadAttachments(g *Scene, u *uploadCursor, dst *ebiten.Image, instances []render.Attachment, grass, underlay bool, budget *uploadBudget) bool {
	if len(instances) == 0 {
		return true
	}
	if !budget.enter(u.stage) {
		return false
	}
	for u.next < len(instances) {
		if !budget.available() {
			return false
		}
		mesh, next := render.AttachmentBatch(instances, u.next, grass, underlay, 16384)
		if !w.prepareAtlas(mesh.Texture, budget) {
			return false
		}
		if len(mesh.Indices) > 0 && !budget.take(len(mesh.Indices)) {
			return false
		}
		g.drawAttachmentMesh(dst, mesh)
		u.next = next
	}
	return true
}

func (w *world) uploadPlantInstances(g *Scene, u *uploadCursor, dst *ebiten.Image, instances []render.Attachment, budget *uploadBudget) bool {
	if !budget.enter(u.stage) {
		return false
	}
	for u.next < len(instances) {
		a := instances[u.next]
		if a.Dead {
			u.next++
			u.instancePart = 0
			continue
		}
		mesh := a.Mesh
		if u.instancePart == 1 {
			mesh = a.Extra
		}
		if len(mesh.Indices) > 0 {
			if !w.prepareAtlas(mesh.Texture, budget) || !budget.take(len(mesh.Indices)) {
				return false
			}
			g.drawAttachmentMesh(dst, mesh)
		}
		if u.instancePart == 0 && len(a.Extra.Indices) > 0 {
			u.instancePart = 1
		} else {
			u.instancePart = 0
			u.next++
		}
	}
	return true
}
