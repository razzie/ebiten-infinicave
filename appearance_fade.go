package infinicave

import "github.com/hajimehoshi/ebiten/v2"

// Preserve the displayed base while completed foreground growth arrives. A
// replacement during an active reveal captures its current appearance once,
// avoiding a flash or a restart of the already visible rock.
func (w *world) beginForegroundReveal(s *worldSection, u *sectionUpload) {
	if w.appearanceFade == nil {
		return
	}
	if s.grass == nil && u.grass != nil {
		s.grassReveal.Start(nil)
	}
	if s.foreground == nil {
		s.foregroundReveal.Start(nil)
		return
	}
	if s.renderWidth() != u.pixels {
		s.foregroundReveal.Clear()
		s.grassReveal.Clear()
		return
	}
	if s.mesh == nil || !s.mesh.ForegroundBaseOnly || u.data.ForegroundBaseOnly {
		return
	}
	previous := s.foreground
	if s.foregroundReveal.Progress() < 1 {
		previous = ebiten.NewImage(s.foreground.Bounds().Dx(), s.foreground.Bounds().Dy())
		w.appearanceFade.DrawReveal(previous, s.foreground, s.foregroundReveal.Previous,
			s.foregroundReveal.Progress(), ebiten.GeoM{})
	} else {
		s.foreground = nil // transfer ownership instead of deallocating at publication
	}
	s.foregroundReveal.Start(previous)
}
