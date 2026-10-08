package terrain

import (
	"image/color"
	"math"
	"math/rand"

	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// ShadeRockFaces prepares a private rendering copy. Generation colors continue
// to steer vegetation; visual normals also drive the rendered depth shadows.
// Polygons and control-point heights are never changed by this material pass.
func ShadeRockFaces(grid RockGrid, boundary []RockEdge, adjacency ...[][]int) RockGrid {
	if len(grid) == 0 {
		return grid
	}
	if !grid[0].Raised {
		return shadeBackgroundRockFaces(grid, adjacency...)
	}
	faces := append(RockGrid(nil), grid...)
	noise := NewPerlin(rand.New(rand.NewSource(0x6661636574)))
	type crest struct {
		a, b, outward geom.V
		light         float64
	}
	var crests []crest
	for _, edge := range boundary {
		if edge.B.Sub(edge.A).Len() < .002 {
			continue // Tiny clipping slivers cannot light a whole neighboring face.
		}
		outward := edge.B.Sub(edge.A).Perp().Norm().Mul(-1)
		world := WorldPoint(grid[edge.Cell].orientation, outward)
		// Undersides and vertical walls never acquire an illuminated top band.
		up := geom.Smoothstep(.15, .75, -world.Y)
		facing := geom.Smoothstep(.1, .65, world.Dot((geom.V{X: rockLight.X, Y: rockLight.Y}).Norm()))
		if light := up * facing; light > 0 {
			crests = append(crests, crest{edge.A, edge.B, outward, light})
		}
	}
	emphasis := make([]float64, len(faces))
	parallelFor(len(faces), func(i int) {
		c := &faces[i]
		// A shared broad field dominates the small independent facet tilt.
		tilt := rockFacetTilt(*c, noise)
		width := geom.Clamp(2*math.Sqrt(geom.PolygonArea(c.Polygon)), .028, .065)
		strongest := 0.0
		var toward geom.V
		for _, edge := range crests {
			if c.Center.X < math.Min(edge.a.X, edge.b.X)-width || c.Center.X > math.Max(edge.a.X, edge.b.X)+width ||
				c.Center.Y < math.Min(edge.a.Y, edge.b.Y)-width || c.Center.Y > math.Max(edge.a.Y, edge.b.Y)+width {
				continue
			}
			d := edge.b.Sub(edge.a)
			q := edge.a.Add(d.Mul(geom.Clamp(c.Center.Sub(edge.a).Dot(d)/math.Max(d.Len2(), 1e-18), 0, 1)))
			if c.Center.Sub(q).Dot(edge.outward) > 1e-7 {
				continue
			}
			weight := edge.light * (1 - geom.Smoothstep(0, width, c.Center.Sub(q).Len()))
			strongest = math.Max(strongest, weight)
			// Blend directions at shared corners. Picking one equally near
			// segment can flip the tilt after harmless window roundoff.
			toward = toward.Add(edge.outward.Mul(weight * d.Len()))
		}
		toward = toward.Norm()
		n := c.Normal
		if n.Z < .05 {
			n = geom.V3{Z: 1}
		}
		// Preserve the relief slope, gently turning the top band toward its lip.
		c.Normal = (geom.V3{X: n.X/n.Z + tilt.X + .32*strongest*toward.X, Y: n.Y/n.Z + tilt.Y + .32*strongest*toward.Y, Z: 1}).Norm()
		emphasis[i] = strongest
	})
	depth := newRockDepth(faces)
	parallelFor(len(faces), func(i int) {
		c := &faces[i]
		c.Shadow, c.Ambient = depth.illumination(*c)
		// Keep some readable flank facets. Depth shapes direct illumination,
		// while ambient light is independently darkened by nearby occluders.
		flank := .72 + .28*geom.Smoothstep(.008, .062, c.Z)
		direct := math.Pow(SurfaceLight(c.Normal, c.orientation), 1.25) * c.Shadow
		direct *= flank * (1 + .12*emphasis[i])
		c.Color = cellColor(.13 + .08*c.Ambient + .66*direct)
		p := RockMaterialPoint(*c, c.Center)
		patina := .95 + .055*noise.Noise(p.X*8+21, p.Y*8+47)
		c.Color = color.NRGBA{
			R: uint8(math.Round(float64(c.Color.R) * patina)),
			G: uint8(math.Round(float64(c.Color.G) * patina * .985)),
			B: uint8(math.Round(float64(c.Color.B) * patina * .955)), A: grid[i].Color.A,
		}
	})
	return faces
}

func rockFacetTilt(c RockCell, noise *Perlin) geom.V {
	p := RockMaterialPoint(c, c.Center)
	key := CollisionVertexKey(p)
	tilt := geom.V{
		X: .16*noise.Noise(p.X*5+17, p.Y*5+31) + .10*(SiteRandom(0x6661636574, key[0], key[1], 0)-.5),
		Y: .16*noise.Noise(p.X*5+73, p.Y*5+97) + .10*(SiteRandom(0x6661636574, key[0], key[1], 1)-.5),
	}
	return InternalPoint(c.orientation, tilt)
}

// The recessed background has no exposed platform crests. Retain its broad
// material field and exact black pockets, then refine its shallow relief with
// correlated normals and restrained contact occlusion. Neighbor probes cover
// the extended side margins as well as the cave's central square.
func shadeBackgroundRockFaces(grid RockGrid, adjacency ...[][]int) RockGrid {
	faces := append(RockGrid(nil), grid...)
	noise := NewPerlin(rand.New(rand.NewSource(0x6661636574)))
	parallelFor(len(faces), func(i int) {
		c := &faces[i]
		tilt := rockFacetTilt(*c, noise).Mul(2)
		n := c.Normal
		if n.Z < .05 {
			n = geom.V3{Z: 1}
		}
		c.Normal = (geom.V3{X: 1.4*n.X/n.Z + tilt.X, Y: 1.4*n.Y/n.Z + tilt.Y, Z: 1}).Norm()
	})
	var neighbors [][]int
	if len(adjacency) > 0 {
		neighbors = adjacency[0]
	}
	if neighbors == nil {
		neighbors = RockNeighbors(faces)
	}
	// Normals are immutable during the second pass; only each worker's own
	// output color/illumination changes. Read neighbor material from the input.
	parallelFor(len(faces), func(i int) {
		c := &faces[i]
		occlusion := 0.0
		for _, j := range neighbors[i] {
			if grid[j].Color.A == 0 || grid[j].Color.R == 0 && grid[j].Color.G == 0 && grid[j].Color.B == 0 {
				continue
			}
			other := RockCell{Center: faces[j].Center, Normal: faces[j].Normal, Z: faces[j].Z}
			d := other.Center.Sub(c.Center)
			mid := c.Center.Add(d.Mul(.5))
			step := RockDepthAt(other, mid) - RockDepthAt(*c, mid) - .0005
			occlusion += geom.Clamp(step/math.Max(.008, d.Len()), 0, .35)
		}
		c.Ambient = 1 - math.Min(.18, 2*occlusion/float64(max(1, len(neighbors[i]))))
		c.Shadow = 1 // Foreground cast shadows remain a dynamic composite pass.
		oldLight := .3 + .7*math.Pow(SurfaceLight(grid[i].Normal.Norm(), c.orientation), 1.25)
		newLight := .3 + .7*math.Pow(SurfaceLight(c.Normal, c.orientation), 1.25)
		p := RockMaterialPoint(*c, c.Center)
		patina := .98 + .04*noise.Noise(p.X*8+21, p.Y*8+47)
		depth := .9 + .1*geom.Smoothstep(-.012, -.004, c.Z)
		strength := newLight / oldLight * c.Ambient * depth * patina
		base := grid[i].Color
		c.Color = color.NRGBA{
			R: uint8(math.Round(geom.Clamp(float64(base.R)*strength, 0, 255))),
			G: uint8(math.Round(geom.Clamp(float64(base.G)*strength, 0, 255))),
			B: uint8(math.Round(geom.Clamp(float64(base.B)*strength, 0, 255))), A: base.A,
		}
	})
	return faces
}
