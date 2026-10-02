package main

import (
	"image/color"
	"math"
)

const foregroundEdgeFadeWidth = 140.0

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
		if g.distanceBound2(p) > 160*160 {
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
		variation := noise.Noise(q.X*.004+17, q.Y*.004+31)
		width := 105 + 35*variation
		elevation := 62 + 14*variation
		bevel := 16 + 4*variation
		along := math.Sqrt(math.Max(0, pr.Dist*pr.Dist-pr.Signed*pr.Signed))
		end := 1 - smoothstep(0, 38, along)
		height = math.Max(height, ridgeProfile(pr.Dist, width, elevation, bevel)*end)
	}
	// Offshoots carry low connected spurs of the same solid rock surface.
	height = math.Max(height, 80*branchBias(p, branches)*branchMask)
	world := V{p.X, p.Y + noise.OffsetY}
	chips := 1.2 * noise.Noise(world.X*.035+43, world.Y*.035+97)
	edgeFade := smoothstep(0, foregroundEdgeFadeWidth, math.Min(p.X, W-p.X))
	return math.Max(0, height+chips*smoothstep(0, 15, height)) * edgeFade
}

// Compress site spacing across the lip and stretch it into the flank. The
// continuous displacement retains irregular sites and creates no aligned rows.
func reliefSeeds(seeds []V, guides []Guide) []V {
	out := append([]V(nil), seeds...)
	for i, p := range out {
		gi, pr := nearestGuide(p, guides)
		if gi < 0 || pr.Signed <= 0 || pr.Dist >= 110 {
			continue
		}
		shift := -15 * math.Sin(math.Pi*pr.Dist/110)
		out[i] = p.Add(pr.N.Mul(shift))
		out[i].X = clamp(out[i].X, 1, W-1)
		out[i].Y = clamp(out[i].Y, 1, H-1)
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

func rockSurfaceColor(normal V3, shadow, ambient float64) color.NRGBA {
	// Material reflectance is separate from visibility and distance to a guide.
	// A low ambient floor keeps flanks charcoal; only lit faces reach warm tan.
	diffuse := math.Pow(surfaceLight(normal), 1.25)
	return cellColor(.12 + .05*ambient + .66*diffuse*shadow)
}

func shadeRockGrids(background, foreground RockGrid, noise *Perlin) {
	depth := newRockDepth(background, foreground)
	for layer, grid := range []RockGrid{background, foreground} {
		parallelFor(len(grid), func(i int) {
			c := &grid[i]
			c.Shadow, c.Ambient = depth.illumination(*c)
			if layer == 1 {
				c.Color = rockSurfaceColor(c.Normal, c.Shadow, c.Ambient)
				// Weathering is coherent across a formation. Lower spurs and
				// recessed feet stay subdued; only the raised crests catch ivory.
				patina := .72 + .28*smoothstep(5, 65, c.Z)
				p := V{c.Center.X, c.Center.Y + noise.OffsetY}
				patina *= .96 + .06*noise.Noise(p.X*.008+21, p.Y*.008+47)
				c.Color.R = uint8(math.Round(float64(c.Color.R) * patina))
				c.Color.G = uint8(math.Round(float64(c.Color.G) * patina * .985))
				c.Color.B = uint8(math.Round(float64(c.Color.B) * patina * .955))
			} else {
				c.Color = backgroundSurfaceColor(c.Center, noise, c.Normal)
				factor := (.3 + .7*c.Shadow) * (.6 + .4*c.Ambient)
				c.Color.R = uint8(math.Round(float64(c.Color.R) * factor))
				c.Color.G = uint8(math.Round(float64(c.Color.G) * factor))
				c.Color.B = uint8(math.Round(float64(c.Color.B) * factor))
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
			remove = .15 * smoothstep(25, 95, pr.Dist) * (1 - smoothstep(190, 260, pr.Dist))
		}
		r := float64(uint64(cellSeed(seed^0x617274, p, top))>>11) / (1 << 53)
		if r >= remove {
			selected = append(selected, p)
		}
	}
	return reliefSeeds(selected, guides)
}
