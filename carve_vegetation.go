package infinicave

import (
	"image"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// Retain CPU plants for support checks, branch remapping, and section reuploads.
// Slices are immutable once published; edits create replacements.
type vegetationGeometry struct {
	vines, foregroundVines []Vine
	mushrooms              []MushroomGroup
	cuts                   []rockCut
}

func sectionVegetation(data sectionData) vegetationGeometry {
	return vegetationGeometry{data.vines, data.foregroundVines, data.mushrooms, data.vegetationCuts}
}

func (cut rockCut) vegetation(source vegetationGeometry, ground RockGrid, top float64) (vegetationGeometry, bool) {
	for _, previous := range source.cuts {
		if sameRockCut(cut, previous) {
			return source, false
		}
	}
	updated := source
	groundFaces := mushroomGroundFromCells(ground)
	var mushrooms []MushroomGroup
	removed := false
	for _, group := range source.mushrooms {
		var kept []Mushroom
		for _, mushroom := range group.Mushrooms {
			if mushroomSupported(mushroom, groundFaces) {
				kept = append(kept, mushroom)
			} else {
				removed = true
			}
		}
		if len(kept) > 0 {
			mushrooms = append(mushrooms, MushroomGroup{Mushrooms: kept})
		}
	}
	if removed {
		updated.mushrooms = mushrooms
	}
	// Ribbons and contact shadows can overlap a hole even when their
	// centerline stays outside it. Keep the mask for the full rendered width.
	var backHit, frontHit bool
	updated.vines, backHit = cut.vines(source.vines, top)
	updated.foregroundVines, frontHit = cut.vines(source.foregroundVines, top)
	if backHit || frontHit {
		updated.cuts = append(append([]rockCut(nil), source.cuts...), cut)
	}
	return updated, removed || backHit || frontHit
}

func (cut rockCut) plants(old *terrainGeometry) (*terrainGeometry, bool) {
	if old == nil {
		return old, false
	}
	vegetation, changed := cut.vegetation(old.vegetation, old.grid, old.top)
	if !changed {
		return old, false
	}
	updated := *old
	updated.vegetation = vegetation
	return &updated, true
}

// Prepare fresh meshes before cropping; finishSectionMesh translates vertices
// in place, so an already cropped mesh must never be finished a second time.
func (g *Scene) prepareCarvedVegetation(mesh *sectionMesh) {
	mesh.vines, mesh.foregroundVines, mesh.mushrooms = nil, nil, triangleMesh{}
	if g.view == ViewShaded {
		plants := mesh.geometry.vegetation
		mesh.vines = prepareVines(plants.vines)
		mesh.foregroundVines = prepareForegroundVines(plants.foregroundVines)
		mesh.mushrooms = prepareMushrooms(plants.mushrooms)
	}
	finishSectionMesh(mesh)
}

func (g *Scene) redrawCarvedVegetation(section *worldSection, id int64) {
	if section.vegetationPending || section.terrain == nil {
		return
	}
	mesh := sectionMesh{id: id, geometry: section.geometry}
	g.prepareCarvedVegetation(&mesh)
	if section.mesh != nil {
		section.mesh.geometry = section.geometry
		section.mesh.vines, section.mesh.foregroundVines, section.mesh.mushrooms = mesh.vines, mesh.foregroundVines, mesh.mushrooms
		section.mesh.vinesBounds, section.mesh.foregroundVinesBounds, section.mesh.mushroomsBounds = mesh.vinesBounds, mesh.foregroundVinesBounds, mesh.mushroomsBounds
	}
	pixels := section.renderWidth()
	mesh = scaleSectionMesh(mesh, pixels)
	if g.world.white == nil {
		g.world.white = ebiten.NewImage(1, 1)
		g.world.white.Fill(color.White)
	}
	for _, layer := range []*ebiten.Image{section.vines, section.foregroundVines, section.mushrooms} {
		if layer != nil {
			layer.Deallocate()
		}
	}
	draw := func(meshes []triangleMesh, bounds image.Rectangle) *ebiten.Image {
		if bounds.Empty() {
			return nil
		}
		img := newVegetationImage(bounds)
		for _, m := range meshes {
			m.draw(img, g.world.white)
		}
		return img
	}
	section.vines = draw(mesh.vines, mesh.vinesBounds)
	section.foregroundVines = draw(mesh.foregroundVines, mesh.foregroundVinesBounds)
	section.mushrooms = draw([]triangleMesh{mesh.mushrooms}, mesh.mushroomsBounds)
	section.vinesBounds, section.foregroundVinesBounds, section.mushroomsBounds = mesh.vinesBounds, mesh.foregroundVinesBounds, mesh.mushroomsBounds
	top := sectionWindowTop(id)
	section.vines = g.finishVinesAt(section.vines, section.vinesBounds, top, pixels)
	for _, layer := range []struct {
		image  *ebiten.Image
		bounds image.Rectangle
	}{
		{section.vines, section.vinesBounds}, {section.foregroundVines, section.foregroundVinesBounds},
	} {
		eraseVineCutsAt(layer.image, layer.bounds, top, section.geometry.vegetation.cuts, pixels)
	}
}

func (g *Scene) finishVinesAt(img *ebiten.Image, bounds image.Rectangle, top float64, pixels int) *ebiten.Image {
	if img == nil || g.vineMaterial == nil {
		return img
	}
	finished := newVegetationImage(bounds)
	finished.DrawRectShader(bounds.Dx(), bounds.Dy(), g.vineMaterial, &ebiten.DrawRectShaderOptions{
		Images: [4]*ebiten.Image{img},
		Uniforms: map[string]any{
			"Offset":  []float32{float32(bounds.Min.X), float32(top*float64(pixels)) + float32(bounds.Min.Y)},
			"Scale":   float32(rasterPixelsPerUnit) / float32(pixels),
			"Texture": float32(g.texture / 8),
		},
	})
	img.Deallocate()
	return finished
}

func sameRockCut(a, b rockCut) bool {
	if len(a.poly) != len(b.poly) {
		return false
	}
	for i, p := range a.poly {
		if p != b.poly[i] {
			return false
		}
	}
	return true
}

func mushroomSupported(m Mushroom, ground []mushroomGround) bool {
	root := m.Anchor
	if len(m.Stem) > 0 {
		root = m.Stem[0]
	}
	// The generated root is sunk into rock. Include the same small contact
	// allowance used by initial placement for jagged guide/contour borders.
	const contact = .002
	for _, cell := range ground {
		if math.Max(root.X, m.Anchor.X)+contact < cell.lo.X || math.Min(root.X, m.Anchor.X)-contact > cell.hi.X ||
			math.Max(root.Y, m.Anchor.Y)+contact < cell.lo.Y || math.Min(root.Y, m.Anchor.Y)-contact > cell.hi.Y {
			continue
		}
		if insideFace(root, cell.poly) || insideFace(m.Anchor, cell.poly) {
			return true
		}
		for i, a := range cell.poly {
			if guideSegmentsDistance2(root, m.Anchor, a, cell.poly[(i+1)%len(cell.poly)]) <= contact*contact {
				return true
			}
		}
	}
	return false
}

// Clip a centerline segment against the convex cut. Boundary-only tangencies
// leave the line intact; the render mask still removes any overlapping ribbon.
func vineCutInterval(a, b V, hole []V) (float64, float64, bool) {
	lo, hi := 0.0, 1.0
	for i, p := range hole {
		n := hole[(i+1)%len(hole)].Sub(p).Perp().Mul(-1).Norm()
		start, end := a.Sub(p).Dot(n), b.Sub(p).Dot(n)
		if math.Abs(start) <= 1e-12 && math.Abs(end) <= 1e-12 {
			return 0, 0, false
		}
		speed := end - start
		if math.Abs(speed) < 1e-15 {
			if start > 0 {
				return 0, 0, false
			}
			continue
		}
		crossing := -start / speed
		if speed < 0 {
			lo = math.Max(lo, crossing)
		} else {
			hi = math.Min(hi, crossing)
		}
		if hi-lo <= 1e-12 {
			return 0, 0, false
		}
	}
	return lo, hi, true
}

type vineFragment struct {
	points []VinePoint
	first  bool // retains the original branch attachment
}

func splitVine(vine Vine, hole []V) []vineFragment {
	holeMin, holeMax := polygonBounds(hole)
	var fragments []vineFragment
	var current []VinePoint
	first := true
	flush := func() {
		if len(current) >= 2 {
			fragments = append(fragments, vineFragment{current, first})
		}
		current = nil
		first = false
	}
	appendPoint := func(p VinePoint) {
		if len(current) == 0 || p.P.Sub(current[len(current)-1].P).Len2() > 1e-24 {
			current = append(current, p)
		}
	}
	for i := 1; i < len(vine.Points); i++ {
		a, b := vine.Points[i-1], vine.Points[i]
		if math.Max(a.P.X, b.P.X) < holeMin.X || math.Min(a.P.X, b.P.X) > holeMax.X ||
			math.Max(a.P.Y, b.P.Y) < holeMin.Y || math.Min(a.P.Y, b.P.Y) > holeMax.Y {
			appendPoint(a)
			appendPoint(b)
			continue
		}
		lo, hi, hit := vineCutInterval(a.P, b.P, hole)
		if !hit {
			appendPoint(a)
			appendPoint(b)
			continue
		}
		point := func(t float64) VinePoint {
			return VinePoint{P: lerpV(a.P, b.P, t), Radius: lerp(a.Radius, b.Radius, t)}
		}
		if lo > 1e-12 {
			appendPoint(a)
			appendPoint(point(lo))
		}
		flush()
		if hi < 1-1e-12 {
			appendPoint(point(hi))
			appendPoint(b)
		}
	}
	flush()
	return fragments
}

func (cut rockCut) vines(source []Vine, top float64) ([]Vine, bool) {
	hit := false
	for _, vine := range source {
		for _, point := range vine.Points {
			if point.P.X+point.Radius+.002 >= cut.min.X && point.P.X-point.Radius-.002 <= cut.max.X &&
				point.P.Y+top+point.Radius+.002 >= cut.min.Y && point.P.Y+top-point.Radius-.002 <= cut.max.Y {
				hit = true
				break
			}
		}
		if hit {
			break
		}
	}
	if !hit {
		// Sparse, long segments may cross the cut between their samples.
		for _, vine := range source {
			for i := 1; i < len(vine.Points); i++ {
				a, b := vine.Points[i-1], vine.Points[i]
				radius := math.Max(a.Radius, b.Radius) + .002
				if math.Max(a.P.X, b.P.X)+radius >= cut.min.X && math.Min(a.P.X, b.P.X)-radius <= cut.max.X &&
					math.Max(a.P.Y, b.P.Y)+top+radius >= cut.min.Y && math.Min(a.P.Y, b.P.Y)+top-radius <= cut.max.Y {
					hit = true
					break
				}
			}
			if hit {
				break
			}
		}
	}
	if !hit {
		return source, false
	}
	hole := cut.localPolygon(top)
	var vines []Vine
	byOriginal := make([][]int, len(source))
	for i, original := range source {
		for _, fragment := range splitVine(original, hole) {
			vine := original
			vine.Foreground = source[vineFamily(source, i)].Foreground
			vine.Points = fragment.points
			vine.Parent, vine.Joint, vine.Depth = -1, 0, 0
			vine.styleFamily, vine.styleSet = vineStyleFamily(source, i), true
			if fragment.first && original.Parent >= 0 && original.Parent < i {
				for _, parent := range byOriginal[original.Parent] {
					for joint, point := range vines[parent].Points {
						if point.P.Sub(vine.Points[0].P).Len2() < 1e-20 {
							vine.Parent, vine.Joint, vine.Depth = parent, joint, vines[parent].Depth+1
							break
						}
					}
					if vine.Parent >= 0 {
						break
					}
				}
			}
			byOriginal[i] = append(byOriginal[i], len(vines))
			vines = append(vines, vine)
		}
	}
	return vines, true
}

// Erase the full ribbon and its shadow after rasterization/blur. Centerline
// clipping alone would leave wide vine edges protruding into the opening.
func eraseVineCutsAt(dst *ebiten.Image, bounds image.Rectangle, windowTop float64, cuts []rockCut, pixels int) {
	if dst == nil || bounds.Empty() {
		return
	}
	for _, cut := range cuts {
		// Clip before converting to float32 pixels, including very large cuts.
		lo := V{float64(bounds.Min.X) / float64(pixels), windowTop + float64(bounds.Min.Y)/float64(pixels)}
		hi := V{float64(bounds.Max.X) / float64(pixels), windowTop + float64(bounds.Max.Y)/float64(pixels)}
		poly := clipHalfPlane(cut.poly, V{-1, 0}, -lo.X)
		poly = clipHalfPlane(poly, V{1, 0}, hi.X)
		poly = clipHalfPlane(poly, V{0, -1}, -lo.Y)
		poly = clipHalfPlane(poly, V{0, 1}, hi.Y)
		if len(poly) < 3 {
			continue
		}
		var path vector.Path
		for i, p := range poly {
			x, y := float32(p.X*float64(pixels)-float64(bounds.Min.X)), float32((p.Y-windowTop)*float64(pixels)-float64(bounds.Min.Y))
			if i == 0 {
				path.MoveTo(x, y)
			} else {
				path.LineTo(x, y)
			}
		}
		path.Close()
		vector.FillPath(dst, &path, nil, &vector.DrawPathOptions{AntiAlias: true, Blend: ebiten.BlendDestinationOut})
	}
}
