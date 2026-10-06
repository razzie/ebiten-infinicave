package infinicave

import "fmt"

// Study selects a full cave or an isolated rock shape.
type Study int

const (
	StudyNone  Study = iota // Full procedural cave.
	StudyLedge              // Isolated ledge, without vines.
	StudyCurl               // Isolated curl, without vines.
)

func (s Study) String() string {
	switch s {
	case StudyNone:
		return ""
	case StudyLedge:
		return "ledge"
	case StudyCurl:
		return "curl"
	default:
		return fmt.Sprintf("Study(%d)", int(s))
	}
}

// ParseStudy converts a viewer flag to a Study. Empty means a full cave.
func ParseStudy(value string) (Study, error) {
	switch value {
	case "":
		return StudyNone, nil
	case "ledge":
		return StudyLedge, nil
	case "curl":
		return StudyCurl, nil
	default:
		return StudyNone, fmt.Errorf("infinicave: unknown study %q", value)
	}
}

// View selects the rendered material or a diagnostic view.
type View int

const (
	ViewShaded  View = iota // Full materials and vegetation.
	ViewClay                // Neutral rock material.
	ViewHeight              // Rock height as grayscale.
	ViewNormals             // Surface normals as RGB.
	ViewShadows             // Shadow visibility as grayscale.
)

func (v View) String() string {
	switch v {
	case ViewShaded:
		return "shaded"
	case ViewClay:
		return "clay"
	case ViewHeight:
		return "height"
	case ViewNormals:
		return "normals"
	case ViewShadows:
		return "shadows"
	default:
		return fmt.Sprintf("View(%d)", int(v))
	}
}

// ParseView converts a viewer flag to a View. Empty means shaded.
func ParseView(value string) (View, error) {
	switch value {
	case "", "shaded":
		return ViewShaded, nil
	case "clay":
		return ViewClay, nil
	case "height":
		return ViewHeight, nil
	case "normals":
		return ViewNormals, nil
	case "shadows":
		return ViewShadows, nil
	default:
		return ViewShaded, fmt.Errorf("infinicave: unknown view %q", value)
	}
}
