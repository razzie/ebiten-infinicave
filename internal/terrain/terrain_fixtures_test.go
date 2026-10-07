package terrain

// CPU-only helpers for terrain generation, collision, and vegetation tests.

import (
	"image/color"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

func testCurlSection(id int64) SectionContent {
	return testRockSection(id, []geom.V{{X: 0.08, Y: 0.65}, {X: 0.38, Y: 0.52}, {X: 0.65, Y: 0.41}, {X: 0.71, Y: 0.26}, {X: 0.57, Y: 0.19}, {X: 0.45, Y: 0.29}, {X: 0.52, Y: 0.37}})
}

func testRockSection(id int64, knots []geom.V) SectionContent {
	guide := RidgedGuide(SplineGuide(knots, 1), 42)
	guide.Seed = SectionSeed(42, id)
	return SectionContent{Guides: []Guide{guide}}
}

func terrainRock(poly []geom.V) RockCell {
	return RockCell{Polygon: poly, Center: geom.PolygonCenter(poly), Raised: true, Color: color.NRGBA{A: 255}}
}

func terrainRect(x, y, width, height float64) RockCell {
	return terrainRock([]geom.V{{X: x, Y: y}, {X: x + width, Y: y}, {X: x + width, Y: y + height}, {X: x, Y: y + height}})
}

func testRockGrid(seeds []geom.V, tones []color.NRGBA) RockGrid {
	return newRockGrid(seeds, func(p geom.V) color.NRGBA {
		for i, seed := range seeds {
			if p == seed {
				return tones[i]
			}
		}
		panic("unknown seed")
	})
}
