package render

import (
	"image/color"
	"math"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

type AtlasKind uint8

const (
	NoAtlas AtlasKind = iota
	GrassAtlas
	MossAtlas
	HangingAtlas
	MushroomStemAtlas
	MushroomCapAtlas
	atlasKinds
)

type atlasSpec struct {
	columns, rows, pixels int
	min, max              geom.V
}

func specForAtlas(kind AtlasKind) atlasSpec {
	switch kind {
	case GrassAtlas:
		return atlasSpec{8, 8, 64, geom.V{X: -.027, Y: -.042}, geom.V{X: .027, Y: .012}}
	case MossAtlas:
		return atlasSpec{8, 8, 48, geom.V{X: -.02, Y: -.02}, geom.V{X: .02, Y: .02}}
	case HangingAtlas:
		return atlasSpec{8, 8, 64, geom.V{X: -.016, Y: -.006}, geom.V{X: .016, Y: .048}}
	default:
		return atlasSpec{16, 16, 128, geom.V{X: -.025, Y: -.040}, geom.V{X: .025, Y: .015}}
	}
}

var atlasRecipes [atlasKinds]struct {
	sync.Once
	meshes []TriangleMesh
}

// AtlasRecipes are immutable CPU samples shared by all scenes and workers.
// Their bounded pages replace detailed meshes repeated in every section.
func AtlasRecipes(kind AtlasKind) []TriangleMesh {
	r := &atlasRecipes[kind]
	r.Do(func() {
		s := specForAtlas(kind)
		r.meshes = make([]TriangleMesh, s.columns*s.rows)
		for cell := range r.meshes {
			var m TriangleMesh
			switch kind {
			case GrassAtlas:
				m = grassSample(cell)
			case MossAtlas:
				m = mossSample(cell)
			case HangingAtlas:
				rock := terrain.RockCell{Polygon: []geom.V{{X: -.02, Y: -.02}, {X: .02, Y: -.02}, {X: .02, Y: -.001}, {X: -.02, Y: -.001}}, Center: geom.V{Y: -.01}, Raised: true, Normal: geom.V3{Z: 1}, Color: color.NRGBA{R: 100, A: 255}}
				e := terrain.RockEdge{A: geom.V{X: .012, Y: float64(cell) * 1e-7}, B: geom.V{X: -.012, Y: float64(cell) * 1e-7}}
				m.Vertices, m.Indices = appendHangingRockGrowth(nil, nil, terrain.RockGrid{rock}, []terrain.RockEdge{e}, false, 1)
				if g, ok := hangingGrowthForEdge(rock, e, 1); ok {
					for j := range m.Vertices {
						m.Vertices[j].DstX -= float32(g.stem[0].X * RasterPixelsPerUnit)
						m.Vertices[j].DstY -= float32(g.stem[0].Y * RasterPixelsPerUnit)
					}
				}
			case MushroomStemAtlas, MushroomCapAtlas:
				m = mushroomSampleMesh(cell, kind == MushroomCapAtlas)
			}
			r.meshes[cell] = m
		}
	})
	return r.meshes
}

func grassSample(cell int) TriangleMesh {
	random := func(c int64) float64 { return terrain.SiteRandom(0x677261737361746c, int64(cell), 0, c) }
	var m TriangleMesh
	clr := color.NRGBA{R: 126, G: 146, B: 65, A: 255}
	if random(0) < .48 {
		clr = color.NRGBA{R: 206, G: 171, B: 88, A: 255}
	}
	// A sample is one blade; roots are placed along the actual surface by the
	// worker, independently of the blade's upward growth direction.
	root := geom.V{}
	height := .010 + .019*random(2)
	bend := (random(3) - .5) * .012
	width := .0008 + .0009*random(4)
	control := root.Add(geom.V{X: bend * .2, Y: -height * .65})
	tip := root.Add(geom.V{X: bend, Y: -height})
	var left, right []geom.V
	for j := 0; j < 4; j++ {
		t := float64(j) / 4
		p := root.Mul((1 - t) * (1 - t)).Add(control.Mul(2 * (1 - t) * t)).Add(tip.Mul(t * t))
		n := control.Sub(root).Mul(1 - t).Add(tip.Sub(control).Mul(t)).Norm().Perp().Mul(width * .5 * (1 - t))
		left = append(left, p.Add(n))
		right = append(right, p.Sub(n))
	}
	poly := append(left, tip)
	for j := len(right) - 1; j >= 0; j-- {
		poly = append(poly, right[j])
	}
	if geom.PolygonArea(poly) < 0 {
		reversePoints(poly)
	}
	m.Vertices, m.Indices = appendCellMesh(nil, nil, poly, geom.PolygonCenter(poly), growthColor(clr, .85+.2*random(5)), geom.V3{Z: 1})
	for i := range m.Vertices {
		v := &m.Vertices[i]
		h := -float64(v.DstY)/RasterPixelsPerUnit - terrain.GenerationMinY
		shade := growthColor(clr, geom.Lerp(.62, 1.12, geom.Clamp(h/height, 0, 1)))
		v.ColorR, v.ColorG, v.ColorB = float32(shade.R)/255, float32(shade.G)/255, float32(shade.B)/255
	}
	return m
}

func mossSample(cell int) TriangleMesh {
	random := func(c int64) float64 { return terrain.SiteRandom(0x6d6f737361746c, int64(cell), 0, c) }
	var m TriangleMesh
	for patch := 0; patch < 2+int(random(1000)*3); patch++ {
		center := geom.V{X: (random(int64(patch)*20) - .5) * .028, Y: (random(int64(patch)*20+1) - .5) * .014}
		r := .014 * geom.Lerp(.38, .85, random(int64(patch)*20+2))
		clr := color.NRGBA{R: 111, G: 128, B: 55, A: 255}
		if random(int64(patch)*20+3) < .43 {
			clr = color.NRGBA{R: 183, G: 164, B: 70, A: 255}
		}
		var poly []geom.V
		for j := 0; j < 9; j++ {
			a := float64(j) * 2 * math.Pi / 9
			size := r * (.65 + .50*random(int64(patch)*20+5+int64(j)))
			poly = append(poly, center.Add(geom.V{X: math.Cos(a) * size, Y: math.Sin(a) * size * .72}))
		}
		m.Vertices, m.Indices = appendCellMesh(m.Vertices, m.Indices, poly, center, growthColor(clr, .87+.21*random(int64(patch)*20+4)), geom.V3{Z: 1})
	}
	return m
}

func mushroomSampleMesh(cell int, caps bool) TriangleMesh {
	var out TriangleMesh
	out.premultiplied = true
	for _, m := range []terrain.Mushroom{terrain.MushroomSpriteSample(cell)} {
		mesh := PrepareMushrooms([]terrain.MushroomGroup{{Mushrooms: []terrain.Mushroom{m}}}, nil)
		first := len(mesh.Vertices) - 2*(1+len(terrain.MushroomCapOutline(m)))
		start, end := 0, first
		if caps {
			start, end = first, len(mesh.Vertices)
		}
		offset := uint32(len(out.Vertices))
		out.Vertices = append(out.Vertices, mesh.Vertices[start:end]...)
		for i := 0; i < len(mesh.Indices); i += 3 {
			if int(mesh.Indices[i]) >= start && int(mesh.Indices[i]) < end {
				for _, v := range mesh.Indices[i : i+3] {
					out.Indices = append(out.Indices, offset+v-uint32(start))
				}
			}
		}
	}
	return out
}

func reversePoints(p []geom.V) {
	for i, j := 0, len(p)-1; i < j; i, j = i+1, j-1 {
		p[i], p[j] = p[j], p[i]
	}
}

// AtlasCache owns GPU pages on the game goroutine. Fixed reference resolution
// bounds storage to about 35 MiB; native scene resizing only changes quads.
type AtlasCache struct {
	Antialiasing AntialiasingMode
	pages        [atlasKinds]*ebiten.Image
	next         [atlasKinds]int
	white        *ebiten.Image
}

// Prepare uploads immutable page batches rather than issuing one draw per
// sample. Callers can park uploads without losing progress.
func (c *AtlasCache) Prepare(kind AtlasKind, take func(int) bool) bool {
	if kind == NoAtlas {
		return true
	}
	s := specForAtlas(kind)
	recipes := AtlasBatches(kind)
	if c.pages[kind] == nil {
		c.pages[kind] = ebiten.NewImage(s.columns*s.pixels, s.rows*s.pixels)
	}
	if c.white == nil {
		c.white = ebiten.NewImage(1, 1)
		c.white.Fill(color.White)
	}
	for c.next[kind] < len(recipes) {
		if !take(len(recipes[c.next[kind]].Indices)) {
			return false
		}
		recipes[c.next[kind]].DrawWithAntialiasing(c.pages[kind], c.white, c.Antialiasing.GeometryAA())
		c.next[kind]++
	}
	return true
}

func (c *AtlasCache) Image(kind AtlasKind) *ebiten.Image { return c.pages[kind] }
func (c *AtlasCache) Close() {
	for i, p := range c.pages {
		if p != nil {
			p.Deallocate()
			c.pages[i] = nil
		}
	}
	if c.white != nil {
		c.white.Deallocate()
		c.white = nil
	}
	c.next = [atlasKinds]int{}
}

var atlasBatches [atlasKinds]struct {
	sync.Once
	meshes []TriangleMesh
}

// AtlasBatches prepares page coordinates on the generation worker. Clipping
// each sample before batching preserves cell isolation and transparent gutters.
func AtlasBatches(kind AtlasKind) []TriangleMesh {
	r := &atlasBatches[kind]
	r.Do(func() {
		s := specForAtlas(kind)
		var meshes []TriangleMesh
		for cell, recipe := range AtlasRecipes(kind) {
			m := TriangleMesh{premultiplied: recipe.premultiplied}
			x, y := float32(cell%s.columns*s.pixels), float32(cell/s.columns*s.pixels)
			vertices := append([]ebiten.Vertex(nil), recipe.Vertices...)
			inside := true
			for j := range vertices {
				v := &vertices[j]
				v.SrcX, v.SrcY = .5, .5
				v.DstX = x + 2 + float32((float64(v.DstX)/RasterPixelsPerUnit-s.min.X)/(s.max.X-s.min.X)*float64(s.pixels-4))
				v.DstY = y + 2 + float32((float64(v.DstY)/RasterPixelsPerUnit+terrain.GenerationMinY-s.min.Y)/(s.max.Y-s.min.Y)*float64(s.pixels-4))
				if v.DstX < x+1 || v.DstX > x+float32(s.pixels-1) || v.DstY < y+1 || v.DstY > y+float32(s.pixels-1) {
					inside = false
				}
			}
			if inside {
				m.Vertices, m.Indices = vertices, recipe.Indices
				meshes = append(meshes, m)
				continue
			}
			for i := 0; i < len(recipe.Indices); i += 3 {
				poly := []ebiten.Vertex{vertices[recipe.Indices[i]], vertices[recipe.Indices[i+1]], vertices[recipe.Indices[i+2]]}
				for axis := 0; axis < 4; axis++ {
					limit := x + 1
					if axis%2 == 1 {
						limit = y + 1
					}
					if axis >= 2 {
						limit += float32(s.pixels - 2)
					}
					poly = clipAtlasPolygon(poly, axis, limit)
				}
				offset := uint32(len(m.Vertices))
				m.Vertices = append(m.Vertices, poly...)
				for j := 1; j+1 < len(poly); j++ {
					m.Indices = append(m.Indices, offset, offset+uint32(j), offset+uint32(j+1))
				}
			}
			meshes = append(meshes, m)
		}
		r.meshes = batchTriangleMeshes(meshes)
	})
	return r.meshes
}

func clipAtlasPolygon(poly []ebiten.Vertex, axis int, limit float32) []ebiten.Vertex {
	if len(poly) == 0 {
		return nil
	}
	coordinate := func(v ebiten.Vertex) float32 {
		if axis%2 == 0 {
			return v.DstX
		}
		return v.DstY
	}
	inside := func(d float32) bool {
		if axis < 2 {
			return d >= 0
		}
		return d <= 0
	}
	var out []ebiten.Vertex
	a := poly[len(poly)-1]
	da := coordinate(a) - limit
	for _, b := range poly {
		db := coordinate(b) - limit
		if inside(da) != inside(db) {
			t := da / (da - db)
			v := a
			v.DstX += (b.DstX - a.DstX) * t
			v.DstY += (b.DstY - a.DstY) * t
			v.ColorR += (b.ColorR - a.ColorR) * t
			v.ColorG += (b.ColorG - a.ColorG) * t
			v.ColorB += (b.ColorB - a.ColorB) * t
			v.ColorA += (b.ColorA - a.ColorA) * t
			out = append(out, v)
		}
		if inside(db) {
			out = append(out, b)
		}
		a, da = b, db
	}
	return out
}
