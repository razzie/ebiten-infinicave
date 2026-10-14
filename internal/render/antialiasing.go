package render

import "fmt"

// AntialiasingMode selects geometry antialiasing or a final scene filter.
type AntialiasingMode uint8

const (
	AntialiasingFull AntialiasingMode = iota
	AntialiasingOff
	AntialiasingFXAA
)

func (m AntialiasingMode) String() string {
	switch m {
	case AntialiasingFull:
		return "full"
	case AntialiasingOff:
		return "off"
	case AntialiasingFXAA:
		return "fxaa"
	default:
		return fmt.Sprintf("AntialiasingMode(%d)", m)
	}
}

func ParseAntialiasingMode(value string) (AntialiasingMode, error) {
	switch value {
	case "", "full":
		return AntialiasingFull, nil
	case "off":
		return AntialiasingOff, nil
	case "fxaa":
		return AntialiasingFXAA, nil
	default:
		return AntialiasingFull, fmt.Errorf("infinicave: unknown antialiasing mode %q: use full, off, or fxaa", value)
	}
}

// GeometryAA reports whether triangle/path draws should enable antialiasing.
func (m AntialiasingMode) GeometryAA() bool { return m == AntialiasingFull }
