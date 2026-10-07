package infinicave

import (
	"image/color"
	"math"
)

const foregroundEdgeFadeWidth = .140

// All terrain lighting, including exposed walls, uses this one light.
var rockLight = (V3{-.28, -.86, .65}).Norm()

// The guide is the exposed lip. Just inside it the bevel rises to a crest,
// then a much wider flank descends toward the surrounding recessed terrain.
func ridgeProfile(distance, width, height, bevel float64) float64 {
	if distance < 0 || distance >= width {
		return 0
	}
	if distance < bevel {
		return height * lerp(.55, 1, distance/bevel)
	}
	// The flank steepens toward the foot, turning into shadow instead of
	// flattening back into a bright row of lower cells.
	return height * math.Pow(1-(distance-bevel)/(width-bevel), .72)
}

func reliefHeight(p V, guides []Guide, noise *Perlin, branches *BranchField) float64 {
	height, branchMask := 0.0, 1.0
	for i := range guides {
		g := &guides[i]
		if g.distanceBound2(p) > 0.16*0.16 {
			continue
		}
		pr := g.project(p)
		if pr.Signed < 0 {
			branchMask *= smoothstep(guideSpacing*2.7, guideSpacing*5, pr.Dist)
			continue
		}
		// Width and elevation vary along the crest, not independently per face.
		q := pr.Q
		q.Y += noise.OffsetY
		variation := noise.Noise(q.X*4+17, q.Y*4+31)
		width := 0.105 + 0.035*variation
		elevation := 0.062 + 0.014*variation
		bevel := 0.016 + 0.004*variation
		along := math.Sqrt(math.Max(0, pr.Dist*pr.Dist-pr.Signed*pr.Signed))
		end := 1 - smoothstep(0, 0.038, along)
		height = math.Max(height, ridgeProfile(pr.Dist, width, elevation, bevel)*end)
	}
	// Offshoots carry low connected spurs of the same solid rock surface.
	height = math.Max(height, 0.08*branchBias(p, branches)*branchMask)
	world := V{p.X, p.Y + noise.OffsetY}
	chips := .0012 * noise.Noise(world.X*35+43, world.Y*35+97)
	edgeFade := smoothstep(0, foregroundEdgeFadeWidth, math.Min(p.X, generationWidth-p.X))
	return math.Max(0, height+chips*smoothstep(0, 0.015, height)) * edgeFade
}

// Compress site spacing across the lip and stretch it into the flank. The
// continuous displacement retains irregular sites and creates no aligned rows.
func reliefSeeds(seeds []V, guides []Guide) []V {
	out := append([]V(nil), seeds...)
	for i, p := range out {
		gi, pr := nearestGuide(p, guides)
		if gi < 0 || pr.Signed <= 0 || pr.Dist >= 0.11 {
			continue
		}
		shift := -0.015 * math.Sin(math.Pi*pr.Dist/0.11)
		out[i] = p.Add(pr.N.Mul(shift))
		out[i].X = clamp(out[i].X, .001, generationWidth-.001)
		out[i].Y = clamp(out[i].Y, generationMinY+.001, generationMaxY-.001)
	}
	return out
}

func shapeReliefGrid(grid RockGrid, guides []Guide, noise *Perlin, branches *BranchField) {
	parallelFor(len(grid), func(i int) {
		grid[i].Z = reliefHeight(grid[i].Center, guides, noise, branches)
		grid[i].Raised = true
	})
	neighbors := rockNeighbors(grid)
	parallelFor(len(grid), func(i int) {
		grid[i].Normal = rockNormal(grid, i, neighbors[i])
		if toward, ok := guideFacing(grid[i], guides); ok {
			grid[i].Normal = (V3{toward.X * 1.4, toward.Y * 1.4, 1}).Norm()
		}
	})
}

func rockSurfaceColor(normal V3, shadow, ambient float64, orientation ...Orientation) color.NRGBA {
	// Material reflectance is separate from visibility and distance to a guide.
	// A low ambient floor keeps flanks charcoal; only lit faces reach warm tan.
	diffuse := math.Pow(surfaceLight(normal, orientation...), 1.25)
	return cellColor(.16 + .05*ambient + .66*diffuse*shadow)
}

func shadeRockGrids(background, foreground RockGrid, noise *Perlin, orientation ...Orientation) {
	mode := optionalOrientation(orientation)
	for _, grid := range []RockGrid{background, foreground} {
		for i := range grid {
			grid[i].orientation = mode
		}
	}
	depth := newRockDepth(background, foreground)
	for layer, grid := range []RockGrid{background, foreground} {
		parallelFor(len(grid), func(i int) {
			c := &grid[i]
			if layer == 1 {
				c.Shadow, c.Ambient = depth.illumination(*c)
				c.Color = rockSurfaceColor(c.Normal, c.Shadow, c.Ambient, mode)
				// Weathering is coherent across a formation. Lower spurs and
				// recessed feet stay subdued; only the raised crests catch ivory.
				patina := .72 + .28*smoothstep(0.005, 0.065, c.Z)
				p := V{c.Center.X, c.Center.Y + noise.OffsetY}
				patina *= .96 + .06*noise.Noise(p.X*8+21, p.Y*8+47)
				c.Color.R = uint8(math.Round(float64(c.Color.R) * patina))
				c.Color.G = uint8(math.Round(float64(c.Color.G) * patina * .985))
				c.Color.B = uint8(math.Round(float64(c.Color.B) * patina * .955))
			} else {
				// Background cast shadows are composited from the current rock
				// silhouette at draw time, including carved openings and vines.
				c.Shadow, c.Ambient = 1, 1
				c.Color = backgroundSurfaceColor(c.Center, noise, c.Normal, mode)
			}
		})
	}
}

// Quiet flanks use fewer, larger facets than the chipped lip. Thinning is
// keyed to world sites so revisiting or overlapping a section changes nothing.
func artisticRockSeeds(seeds []V, guides []Guide, seed int64, top float64) []V {
	selected := make([]V, 0, len(seeds))
	for _, p := range seeds {
		gi, pr := nearestGuide(p, guides)
		remove := 0.0
		if gi >= 0 && pr.Signed > 0 {
			remove = .15 * smoothstep(0.025, 0.095, pr.Dist) * (1 - smoothstep(0.19, 0.26, pr.Dist))
		}
		r := float64(uint64(cellSeed(seed^0x617274, p, top))>>11) / (1 << 53)
		if r >= remove {
			selected = append(selected, p)
		}
	}
	return reliefSeeds(selected, guides)
}
