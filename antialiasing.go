package infinicave

import "github.com/razzie/ebiten-infinicave/internal/render"

// AntialiasingMode selects geometry antialiasing or a final scene filter.
type AntialiasingMode = render.AntialiasingMode

const (
	AntialiasingFull = render.AntialiasingFull
	AntialiasingOff  = render.AntialiasingOff
	AntialiasingFXAA = render.AntialiasingFXAA
)

// ParseAntialiasingMode accepts full, off, and fxaa. Empty selects full.
func ParseAntialiasingMode(value string) (AntialiasingMode, error) {
	return render.ParseAntialiasingMode(value)
}
