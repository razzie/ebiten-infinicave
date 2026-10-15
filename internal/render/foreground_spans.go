package render

import (
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// ForegroundSpan keeps the original attachment indices and support index while
// masking unrelated draws. Independent uploads and edit replay use the same IDs.
type ForegroundSpan uint8

const (
	StructuralSpan ForegroundSpan = iota
	StructuralTopSpan
	UnderGrowthSpan
	SurfaceGrowthSpan
	GrassSpan
)

func (span ForegroundSpan) Contains(a Attachment) bool {
	switch span {
	case StructuralSpan:
		return a.Kind == EdgeAttachment
	case StructuralTopSpan:
		return a.Kind == EdgeAttachment && !a.Underlay
	case GrassSpan:
		return a.Kind == GrassAttachment
	case UnderGrowthSpan:
		return a.Kind != EdgeAttachment && a.Kind != GrassAttachment && a.Underlay
	default:
		return a.Kind != EdgeAttachment && a.Kind != GrassAttachment && !a.Underlay
	}
}

func MaskForeground(grid GridMesh, span ForegroundSpan) GridMesh {
	grid.Growth = append([]Attachment(nil), grid.Growth...)
	for i := range grid.Growth {
		if !span.Contains(grid.Growth[i]) {
			grid.Growth[i].Dead = true
		}
	}
	return grid
}

func HasForegroundSpan(grid GridMesh, span ForegroundSpan) bool {
	for _, a := range grid.Growth {
		if !a.Dead && span.Contains(a) && (len(a.Mesh.Indices) > 0 || len(a.Extra.Indices) > 0) {
			return true
		}
	}
	return false
}

func StructuralSection(mesh SectionMesh) SectionMesh {
	mesh.Foreground = MaskForeground(mesh.Foreground, StructuralSpan)
	return mesh
}

// PrepareForegroundGrowth reuses every structural triangle and attachment from
// the early base. Only growth fitting needs the refined appearance boundary.
func PrepareForegroundGrowth(data terrain.SectionData, view View, base SectionMesh, options ...RockGrowthOptions) SectionMesh {
	mesh := base
	mesh.Layers, mesh.ForegroundBaseOnly = ForegroundLayer, false
	if view == ViewShaded {
		density := sectionRockGrowth(data, options)
		topology := sectionBakeTopology(data)
		if topology == nil {
			topology = terrain.NewRockTopology(data.Foreground)
		}
		topology = terrain.PrepareRockAppearance(topology)
		grid, boundary := topology.Appearance, topology.Boundary
		grid, boundary, _ = visualBoundaryGrid(grid, boundary, density, true, view)
		growth := prepareGrowthAttachments(grid, boundary, density, true)
		stampGrowthIDs(growth, grid)
		var ordered []Attachment
		for _, a := range growth {
			if a.Underlay {
				ordered = append(ordered, a)
			}
		}
		for _, a := range base.Foreground.Growth {
			if a.Underlay {
				ordered = append(ordered, a)
			}
		}
		for _, a := range growth {
			if !a.Underlay {
				ordered = append(ordered, a)
			}
		}
		for _, a := range base.Foreground.Growth {
			if !a.Underlay {
				ordered = append(ordered, a)
			}
		}
		mesh.Foreground.Growth = ordered
		mesh.Foreground.GrowthIndex = AttachmentIndex(ordered)
	}
	applyAuthoredVisualCuts(&mesh, data)
	return mesh
}

func stampGrowthIDs(attachments []Attachment, grid terrain.RockGrid) {
	if len(grid) == 0 {
		return
	}
	for i := range attachments {
		a := &attachments[i]
		p := geom.LerpVector(a.Min, a.Max, .5)
		if len(a.Anchors) > 0 {
			p = a.Anchors[0]
		}
		a.ID = terrain.CollisionVertexKey(terrain.RockMaterialPoint(grid[0], p))
	}
}
