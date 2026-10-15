package infinicave

import "github.com/razzie/ebiten-infinicave/internal/render"

// Rock publication owns the structural reveal. Growth has separate clocks
// and never replaces or restarts the displayed rock image.
func (w *world) beginForegroundReveal(s *worldSection, u *sectionUpload) {
	if w.appearanceFade == nil {
		return
	}
	if s.foreground == nil {
		s.foregroundReveal.Start(nil)
		return
	}
	if s.renderWidth() != u.pixels {
		s.foregroundReveal.Clear()
		return
	}
}

func (s *worldSection) displayedRockSource() *render.SectionMesh {
	if s.rockSource != nil {
		return s.rockSource
	}
	return s.mesh
}
