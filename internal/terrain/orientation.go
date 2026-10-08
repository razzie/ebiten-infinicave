package terrain

import (
	"fmt"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// Orientation selects the cave's fixed scrolling axis. Vertical grows upward
// from Y = 0; Horizontal grows rightward from X = 0. The bounded axis spans
// [0, 1]. Section IDs are 0, -1, -2, ... in the direction of growth.
type Orientation uint8

const (
	Vertical Orientation = iota
	Horizontal
)

func OptionalOrientation(options []Orientation) Orientation {
	if len(options) > 0 {
		return options[0]
	}
	return Vertical
}

func (o Orientation) String() string {
	switch o {
	case Vertical:
		return "vertical"
	case Horizontal:
		return "horizontal"
	default:
		return fmt.Sprintf("Orientation(%d)", o)
	}
}

// Generation and streaming share a section frame: X is across the cave and
// negative Y is forward. A proper rotation preserves polygon winding, lengths,
// and normals. Artistic directions are converted into this frame before
// generation; public geometry is converted back into ordinary world X/Y.
func WorldPoint(o Orientation, p geom.V) geom.V {
	if o == Horizontal {
		return geom.V{X: -p.Y, Y: p.X}
	}
	return p
}

func InternalPoint(o Orientation, p geom.V) geom.V {
	if o == Horizontal {
		return geom.V{X: p.Y, Y: -p.X}
	}
	return p
}

func (o Orientation) local(p geom.V) geom.V {
	if o == Horizontal {
		return geom.V{X: 1 - p.Y, Y: p.X}
	}
	return p
}

func (o Orientation) internalLocal(p geom.V) geom.V {
	if o == Horizontal {
		return geom.V{X: p.Y, Y: 1 - p.X}
	}
	return p
}

func WorldNormal(o Orientation, n geom.V3) geom.V3 {
	p := WorldPoint(o, geom.V{X: n.X, Y: n.Y})
	return geom.V3{X: p.X, Y: p.Y, Z: n.Z}
}

func WorldBounds(o Orientation, lo, hi geom.V) (geom.V, geom.V) {
	if o == Horizontal {
		return geom.V{X: -hi.Y, Y: lo.X}, geom.V{X: -lo.Y, Y: hi.X}
	}
	return lo, hi
}

func (o Orientation) sectionOrigin(id int64) geom.V {
	if o == Horizontal {
		return geom.V{X: float64(-id)}
	}
	return geom.V{Y: float64(id) - 1}
}

func MapPoints(points []geom.V, transform func(geom.V) geom.V) {
	for i, p := range points {
		points[i] = transform(p)
	}
}

func MapHole(h Hole, transform func(geom.V) geom.V) Hole {
	if h.Shape == HoleCircle {
		h.Center = transform(h.Center)
	} else {
		h.Start, h.End = transform(h.Start), transform(h.End)
	}
	return h
}

func OrientedCollision(o Orientation, geometry CollisionGeometry) CollisionGeometry {
	geometry = CopyCollisionGeometry(geometry)
	geometry.Origin = o.sectionOrigin(geometry.ID)
	geometry.Min, geometry.Max = geometry.Origin, geometry.Origin.Add(geom.V{X: 1, Y: 1})
	if o == Horizontal {
		geometry.Top = 0 // Legacy vertical metadata; use Origin or Min/Max.
		for _, poly := range geometry.Polygons {
			MapPoints(poly, func(point geom.V) geom.V {
				return WorldPoint(o, point)
			})
		}
	}
	return geometry
}

func OrientedSectionLoader(o Orientation, load SectionLoader) SectionLoader {
	if o == Vertical || load == nil {
		return load
	}
	return func(id int64) SectionContent {
		content := load(id)
		guides := make([]Guide, len(content.Guides))
		for i, g := range content.Guides {
			g.Pts = append([]geom.V(nil), g.Pts...)
			MapPoints(g.Pts, o.internalLocal)
			guides[i] = g // loadedGuide recomputes lengths and bounds.
		}
		holes := append([]Hole(nil), content.Holes...)
		for i, h := range holes {
			holes[i] = MapHole(h, o.internalLocal)
		}
		return SectionContent{Guides: guides, Holes: holes}
	}
}

// All slices here belong to the synchronous generation caller.
func OrientedSection(o Orientation, s Section) Section {
	s.Orientation = o
	s.Origin = o.sectionOrigin(s.ID)
	s.Min, s.Max = s.Origin, s.Origin.Add(geom.V{X: 1, Y: 1})
	s.WindowOrigin = s.Origin.Add(WorldPoint(o, geom.V{Y: -1}))
	if o == Vertical {
		return s
	}
	s.Top, s.WindowTop = 0, 0
	s.WindowOrigin = s.Origin.Sub(geom.V{X: 1})
	for _, grid := range []RockGrid{s.Background, s.Foreground} {
		for i := range grid {
			c := &grid[i]
			c.Center = o.local(c.Center)
			MapPoints(c.Polygon, o.local)
			c.Normal = WorldNormal(o, c.Normal)
			c.orientation = Vertical // The returned normals use world axes.
		}
	}
	for _, vines := range [][]Vine{s.Vines, s.ForegroundVines} {
		for i := range vines {
			for j := range vines[i].Points {
				vines[i].Points[j].P = o.local(vines[i].Points[j].P)
			}
		}
	}
	for _, group := range s.Mushrooms {
		for i := range group.Mushrooms {
			m := &group.Mushrooms[i]
			MapPoints(m.Stem, o.local)
			m.Anchor, m.CapCenter = o.local(m.Anchor), o.local(m.CapCenter)
			m.RootDirection = WorldPoint(o, m.RootDirection)
			m.orientation = Vertical
		}
	}
	for i := range s.Guides {
		g := &s.Guides[i]
		g.projection = nil
		MapPoints(g.Pts, o.local)
		g.Min, g.Max = geom.PolygonBounds(g.Pts)
	}
	for i, h := range s.Holes {
		s.Holes[i] = MapHole(h, o.local)
	}
	return s
}
