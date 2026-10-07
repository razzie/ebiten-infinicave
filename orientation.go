package infinicave

import "fmt"

// Orientation selects the cave's fixed scrolling axis. Vertical grows upward
// from Y = 0; Horizontal grows rightward from X = 0. The bounded axis spans
// [0, 1]. Section IDs are 0, -1, -2, ... in the direction of growth.
type Orientation uint8

const (
	Vertical Orientation = iota
	Horizontal
)

func optionalOrientation(options []Orientation) Orientation {
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
func (o Orientation) world(p V) V {
	if o == Horizontal {
		return V{-p.Y, p.X}
	}
	return p
}

func (o Orientation) internal(p V) V {
	if o == Horizontal {
		return V{p.Y, -p.X}
	}
	return p
}

func (o Orientation) local(p V) V {
	if o == Horizontal {
		return V{1 - p.Y, p.X}
	}
	return p
}

func (o Orientation) internalLocal(p V) V {
	if o == Horizontal {
		return V{p.Y, 1 - p.X}
	}
	return p
}

func (o Orientation) normal(n V3) V3 {
	p := o.world(V{n.X, n.Y})
	return V3{p.X, p.Y, n.Z}
}

func (o Orientation) bounds(lo, hi V) (V, V) {
	if o == Horizontal {
		return V{-hi.Y, lo.X}, V{-lo.Y, hi.X}
	}
	return lo, hi
}

func (o Orientation) sectionOrigin(id int64) V {
	if o == Horizontal {
		return V{X: float64(-id)}
	}
	return V{Y: float64(id) - 1}
}

func (o Orientation) viewport(v Viewport) (Viewport, bool) {
	if o == Horizontal {
		// Validate X independently: adding Width must not hide a negative X.
		if !finiteCarveValue(v.X) || v.X < 0 || !finiteCarveValue(v.Width) || v.Width <= 0 {
			return Viewport{}, false
		}
		v = Viewport{Y: -(v.X + v.Width), Height: v.Width, Velocity: -v.Velocity}
	}
	return v, v.valid()
}

func mapPoints(points []V, transform func(V) V) {
	for i, p := range points {
		points[i] = transform(p)
	}
}

func mapHole(h Hole, transform func(V) V) Hole {
	if h.Shape == HoleCircle {
		h.Center = transform(h.Center)
	} else {
		h.Start, h.End = transform(h.Start), transform(h.End)
	}
	return h
}

func (o Orientation) collision(geometry CollisionGeometry) CollisionGeometry {
	geometry = copyCollisionGeometry(geometry)
	geometry.Origin = o.sectionOrigin(geometry.ID)
	geometry.Min, geometry.Max = geometry.Origin, geometry.Origin.Add(V{1, 1})
	if o == Horizontal {
		geometry.Top = 0 // Legacy vertical metadata; use Origin or Min/Max.
		for _, poly := range geometry.Polygons {
			mapPoints(poly, o.world)
		}
	}
	return geometry
}

func (o Orientation) sectionLoader(load SectionLoader) SectionLoader {
	if o == Vertical || load == nil {
		return load
	}
	return func(id int64) SectionContent {
		content := load(id)
		guides := make([]Guide, len(content.Guides))
		for i, g := range content.Guides {
			g.Pts = append([]V(nil), g.Pts...)
			mapPoints(g.Pts, o.internalLocal)
			guides[i] = g // loadedGuide recomputes lengths and bounds.
		}
		holes := append([]Hole(nil), content.Holes...)
		for i, h := range holes {
			holes[i] = mapHole(h, o.internalLocal)
		}
		return SectionContent{Guides: guides, Holes: holes}
	}
}

// All slices here belong to the synchronous generation caller.
func (o Orientation) section(s Section) Section {
	s.Orientation = o
	s.Origin = o.sectionOrigin(s.ID)
	s.Min, s.Max = s.Origin, s.Origin.Add(V{1, 1})
	s.WindowOrigin = s.Origin.Add(o.world(V{Y: -1}))
	if o == Vertical {
		return s
	}
	s.Top, s.WindowTop = 0, 0
	s.WindowOrigin = s.Origin.Sub(V{X: 1})
	for _, grid := range []RockGrid{s.Background, s.Foreground} {
		for i := range grid {
			c := &grid[i]
			c.Center = o.local(c.Center)
			mapPoints(c.Polygon, o.local)
			c.Normal = o.normal(c.Normal)
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
			mapPoints(m.Stem, o.local)
			m.Anchor, m.CapCenter = o.local(m.Anchor), o.local(m.CapCenter)
			m.RootDirection = o.world(m.RootDirection)
			m.orientation = Vertical
		}
	}
	for i := range s.Guides {
		g := &s.Guides[i]
		mapPoints(g.Pts, o.local)
		g.Min, g.Max = polygonBounds(g.Pts)
	}
	for i, h := range s.Holes {
		s.Holes[i] = mapHole(h, o.local)
	}
	return s
}
