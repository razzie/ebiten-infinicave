package infinicave

// sectionPoint converts the padded generation window to a section-local point.
// The middle band of the window is the owned unit square.
func sectionPoint(p V) V {
	return V{X: p.X / generationWidth, Y: (p.Y - sectionHeight) / generationWidth}
}

func normalizeSectionPoints(points []V) {
	for i, p := range points {
		points[i] = sectionPoint(p)
	}
}

func (s *Section) normalize() {
	s.Top /= generationWidth
	s.WindowTop /= generationWidth
	for _, grid := range []RockGrid{s.Background, s.Foreground} {
		for i := range grid {
			grid[i].Center = sectionPoint(grid[i].Center)
			normalizeSectionPoints(grid[i].Polygon)
			grid[i].Z /= generationWidth
		}
	}
	for _, vines := range [][]Vine{s.Vines, s.ForegroundVines} {
		for i := range vines {
			for j := range vines[i].Points {
				point := &vines[i].Points[j]
				point.P = sectionPoint(point.P)
				point.Radius /= generationWidth
			}
		}
	}
	for i := range s.Mushrooms {
		for j := range s.Mushrooms[i].Mushrooms {
			m := &s.Mushrooms[i].Mushrooms[j]
			normalizeSectionPoints(m.Stem)
			m.Anchor = sectionPoint(m.Anchor)
			m.CapCenter = sectionPoint(m.CapCenter)
			m.CapWidth /= generationWidth
			m.CapHeight /= generationWidth
		}
	}
	for i := range s.Guides {
		g := &s.Guides[i]
		normalizeSectionPoints(g.Pts)
		g.Min, g.Max = sectionPoint(g.Min), sectionPoint(g.Max)
		for j := range g.S {
			g.S[j] /= generationWidth
		}
	}
	s.Collision.normalize()
}

// Collision boundaries are world points; copy them before exposing cached data.
func (g *CollisionGeometry) normalize() {
	g.ID = -g.ID
	g.Top /= generationWidth
	polygons := make([][]V, len(g.Polygons))
	for i, poly := range g.Polygons {
		polygons[i] = make([]V, len(poly))
		for j, p := range poly {
			polygons[i][j] = p.Mul(1.0 / generationWidth)
		}
	}
	g.Polygons = polygons
}
