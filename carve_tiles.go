package infinicave

import (
	"image"
	"slices"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/render"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Union dirty tiles and join adjacent runs into rectangles. Large blasts then
// replay each mesh and cut mask once per rectangle rather than once per tile,
// while gaps between unrelated attachments stay untouched.
func dirtyTiles(dst *ebiten.Image, bounds image.Rectangle, top float64, pixels int, regions []geom.Bounds) []geom.Bounds {
	if dst == nil {
		return nil
	}
	const tile = 64
	seen := make(map[image.Point]bool)
	var tiles []image.Point
	for _, region := range regions {
		r := dirtyRaster(bounds, top, pixels, region, dst)
		for y := r.Min.Y; y < r.Max.Y; y += tile {
			for x := r.Min.X; x < r.Max.X; x += tile {
				p := image.Pt(x, y)
				if !seen[p] {
					seen[p] = true
					tiles = append(tiles, p)
				}
			}
		}
	}
	slices.SortFunc(tiles, func(a, b image.Point) int {
		if a.Y != b.Y {
			return a.Y - b.Y
		}
		return a.X - b.X
	})
	var rects []image.Rectangle
	previous := make(map[[2]int]int)
	for i := 0; i < len(tiles); {
		y := tiles[i].Y
		current := make(map[[2]int]int)
		for i < len(tiles) && tiles[i].Y == y {
			x, end := tiles[i].X, tiles[i].X+tile
			i++
			for i < len(tiles) && tiles[i].Y == y && tiles[i].X == end {
				end += tile
				i++
			}
			key := [2]int{x, end}
			index, ok := previous[key]
			if ok && rects[index].Max.Y == y {
				rects[index].Max.Y = y + tile
			} else {
				index = len(rects)
				rects = append(rects, image.Rect(x, y, end, y+tile))
			}
			current[key] = index
		}
		previous = current
	}
	result := make([]geom.Bounds, 0, len(rects))
	for _, rect := range rects {
		r := rect.Intersect(dst.Bounds()).Add(bounds.Min)
		// Interior pixel coordinates avoid floating-point roundoff snapping a
		// nominal tile edge into its neighboring tile in dirtyRaster.
		result = append(result, geom.Bounds{
			Min: V{X: (float64(r.Min.X) + .25) / float64(pixels), Y: top + (float64(r.Min.Y)+.25)/float64(pixels)},
			Max: V{X: (float64(r.Max.X) - .25) / float64(pixels), Y: top + (float64(r.Max.Y)-.25)/float64(pixels)},
		})
	}
	return result
}

func (g *Scene) replayGridRegions(section *worldSection, id int64, regions []geom.Bounds, grass bool) {
	pixels := section.renderWidth()
	dst := section.foreground
	if grass {
		dst = section.grass
	}
	for _, dirty := range dirtyTiles(dst, image.Rect(0, pixels, pixels, 2*pixels), terrain.SectionWindowTop(id), pixels, regions) {
		g.replayGridRegion(section, id, dirty, grass)
	}
}

func (g *Scene) replayAttachmentRegions(dst *ebiten.Image, source []render.Attachment, index *render.AttachmentSupportIndex, referenceBounds, nativeBounds image.Rectangle, id int64, pixels int, regions []geom.Bounds) {
	for _, dirty := range dirtyTiles(dst, nativeBounds, terrain.SectionWindowTop(id), pixels, regions) {
		g.replayAttachmentRegion(dst, source, index, referenceBounds, nativeBounds, id, pixels, dirty)
	}
}

// Raster geometry and atlas coordinates do not change when an instance dies.
func markRasterDeaths(source []render.Attachment, removed []int) []render.Attachment {
	if len(source) == 0 || len(removed) == 0 {
		return source
	}
	out := append([]render.Attachment(nil), source...)
	for _, i := range removed {
		out[i].Dead = true
	}
	return out
}
