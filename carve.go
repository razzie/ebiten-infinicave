package infinicave

import (
	"fmt"
	"image/color"
	"math"
	"sort"

	"github.com/hajimehoshi/ebiten/v2"
)

// FormationChange maps an affected formation to its remaining connected parts.
// No Remaining IDs means its cached portion was destroyed; two or more means
// the cached geometry split.
// Before expires even when only one part remains. Complete is false when
// uncached continuation could change the number or connectivity of those parts.
type FormationChange struct {
	Before    FormationID
	Remaining []FormationID
	Complete  bool
}

// CarveResult describes immediate changes to cached foreground rock.
// SectionIDs are the edited collision sections (0, -1, -2, ...). Refetch their
// collision geometry after carving. Complete requires the cut's world area and
// every affected formation to be loaded. Cuts also apply to future section
// loads, whose changes cannot be included in this immediate result.
type CarveResult struct {
	Changes    []FormationChange
	SectionIDs []int64
	Complete   bool
}

// CarveCircle removes foreground rock in a blast centered at a world point.
// Radius must be finite and positive. The circle uses a 96-sided inscribed
// polygon (maximum radial error about 0.00054 * radius). Rendering, exact
// queries, hover, and collision use the same cut. Unsupported mushrooms are
// removed and both vine layers are cut. Background rock is retained.
// Edits persist until Reset or Close, including across eviction.
// Call on the game goroutine after Update. Closed scenes return an error.
func (g *Scene) CarveCircle(center V, radius float64) (CarveResult, error) {
	return g.Carve(Hole{Shape: HoleCircle, Center: center, Radius: radius})
}

// CarveSegment removes a rectangle along Start to End in world coordinates.
// Width is the full thickness, perpendicular to the segment; ends are flat.
// Endpoints must be distinct and finite, and width finite and positive.
// It has the same lifetime, threading, and result semantics as CarveCircle.
func (g *Scene) CarveSegment(start, end V, width float64) (CarveResult, error) {
	return g.Carve(Hole{Shape: HoleSegment, Start: start, End: end, Width: width})
}

// Carve applies a Hole in world coordinates. It is also the common entry point
// for saved or externally supplied edits; SectionLoader uses local coordinates.
func (g *Scene) Carve(hole Hole) (CarveResult, error) {
	cut, err := hole.rockCut()
	if err != nil {
		return CarveResult{}, err
	}
	return g.carve(cut)
}

func finiteCarveValue(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }
func finiteCarvePoint(p V) bool       { return finiteCarveValue(p.X) && finiteCarveValue(p.Y) }

type rockCut struct {
	poly     []V
	min, max V
}

func (g *Scene) carve(cut rockCut) (CarveResult, error) {
	if g.closed || g.world == nil {
		return CarveResult{}, fmt.Errorf("infinicave: cannot carve a closed scene")
	}
	if cut.max.X <= 0 || cut.min.X >= Width || cut.min.Y >= 0 {
		return CarveResult{Complete: true}, nil
	}
	w := g.world
	q := w.queryIndex()
	result := CarveResult{Complete: cut.covered(q.sections)}
	affected := make(map[uint64]bool)
	// Track each replacement face's origin, including unchanged faces from
	// the same formation. This maps both sides of a split to their parent.
	origins := make(map[[2]int64]uint64)
	for _, id := range q.sections {
		section := w.sections[id]
		old := section.geometry
		updated, parents, changed := cut.geometry(id, old, g.collisionTolerance)
		updated, plantsChanged := cut.plants(updated)
		if plantsChanged {
			section.geometry = updated
			g.redrawCarvedVegetation(section, id)
		}
		if !changed {
			for _, face := range old.faces {
				origins[face.key] = q.faceIDs[face.key]
			}
			continue
		}
		for i, parent := range parents {
			origins[updated.faces[i].key] = q.faceIDs[old.faces[parent].key]
		}
		// Only actual removed area marks a formation affected, not a bounds
		// overlap or a boundary touch.
		for i, face := range old.faces {
			if cut.intersects(face.poly, old.top) {
				affected[q.faceIDs[old.faces[i].key]] = true
			}
		}
		section.geometry = updated
		g.redrawCarvedSection(section)
		result.SectionIDs = append(result.SectionIDs, -id)
	}
	delete(affected, 0) // padding-only geometry has no query ID
	before := make(map[uint64]Formation)
	for id := range affected {
		before[id] = q.formations[id]
		// Previous identity must not reconnect newly separated components.
		for key, previous := range q.faceIDs {
			if previous == id {
				delete(q.faceIDs, key)
			}
		}
		for alias, canonical := range q.aliases {
			if canonical == id {
				delete(q.aliases, alias)
			}
		}
	}
	w.cuts = append(w.cuts, cut)
	g.carveUpload(cut)
	w.revision++
	q = w.queryIndex()
	remaining := make(map[uint64]map[uint64]bool)
	for key, parent := range origins {
		if !affected[parent] || q.faceIDs[key] == 0 {
			continue
		}
		if remaining[parent] == nil {
			remaining[parent] = make(map[uint64]bool)
		}
		remaining[parent][q.faceIDs[key]] = true
	}
	ids := make([]uint64, 0, len(affected))
	for id := range affected {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		change := FormationChange{Before: before[id].ID, Complete: before[id].Complete}
		for part := range remaining[id] {
			f := q.formations[part]
			change.Remaining = append(change.Remaining, f.ID)
			change.Complete = change.Complete && f.Complete
		}
		sort.Slice(change.Remaining, func(i, j int) bool { return change.Remaining[i].object < change.Remaining[j].object })
		result.Changes = append(result.Changes, change)
		result.Complete = result.Complete && change.Complete
	}
	return result, nil
}

// Check section coverage with intervals, avoiding a walk through potentially
// enormous unloaded ranges for large cuts. Out-of-world area is known empty.
func (cut rockCut) covered(sections []int64) bool {
	if cut.max.X <= 0 || cut.min.X >= Width || cut.min.Y >= 0 {
		return true
	}
	end := math.Min(0, cut.max.Y)
	for i := len(sections) - 1; i >= 0; i-- {
		top := sectionTop(sections[i])
		if top+SectionHeight <= cut.min.Y || top >= end {
			continue
		}
		if top > cut.min.Y+queryEpsilon {
			return false
		}
		cut.min.Y = top + SectionHeight
		if cut.min.Y >= end-queryEpsilon {
			return true
		}
	}
	return false
}

func (cut rockCut) localPolygon(top float64) []V {
	poly := make([]V, len(cut.poly))
	for i, p := range cut.poly {
		poly[i] = p.Sub(V{Y: top})
	}
	return poly
}

func cutIntersection(poly, hole []V) []V {
	for i, a := range hole {
		normal := hole[(i+1)%len(hole)].Sub(a).Perp().Mul(-1).Norm()
		poly = clipHalfPlane(poly, normal, a.Dot(normal))
		if len(poly) < 3 {
			return nil
		}
	}
	return poly
}

func convexCarveFace(poly []V) bool {
	for i, a := range poly {
		b, c := poly[(i+1)%len(poly)], poly[(i+2)%len(poly)]
		if cross(b.Sub(a), c.Sub(b)) < -1e-16 {
			return false
		}
	}
	return true
}

func (cut rockCut) intersects(poly []V, top float64) bool {
	lo, hi := polygonBounds(poly)
	if hi.X <= cut.min.X || lo.X >= cut.max.X || hi.Y+top <= cut.min.Y || lo.Y+top >= cut.max.Y {
		return false
	}
	hole := cut.localPolygon(top)
	if convexCarveFace(poly) {
		return faceArea(cutIntersection(poly, hole)) > 1e-15
	}
	for _, tri := range faceTriangles(poly) {
		if faceArea(cutIntersection([]V{poly[tri[0]], poly[tri[1]], poly[tri[2]]}, hole)) > 1e-15 {
			return true
		}
	}
	return false
}

// Subtract a convex hole by peeling off the outside of each half-plane.
// Each piece is disjoint; the final inside remainder is discarded. Triangulate
// concave source faces first so half-plane clipping cannot bridge concavities.
func subtractRockCut(poly, hole []V) [][]V {
	sources := [][]V{poly}
	if !convexCarveFace(poly) {
		sources = nil
		for _, tri := range faceTriangles(poly) {
			sources = append(sources, []V{poly[tri[0]], poly[tri[1]], poly[tri[2]]})
		}
	}
	var pieces [][]V
	for _, source := range sources {
		inside := source
		for i, a := range hole {
			normal := hole[(i+1)%len(hole)].Sub(a).Perp().Mul(-1).Norm()
			outside := clipHalfPlane(inside, normal.Mul(-1), -a.Dot(normal))
			if len(outside) >= 3 && faceArea(outside) > 1e-15 {
				pieces = append(pieces, outside)
			}
			inside = clipHalfPlane(inside, normal, a.Dot(normal))
			if len(inside) < 3 || faceArea(inside) <= 1e-15 {
				break
			}
		}
	}
	// Remove partition edges within the original facet where possible. A ring
	// stays as multiple simple faces; joinFaces never fills an enclosed hole.
	for i := 0; i < len(pieces); i++ {
		for j := i + 1; j < len(pieces); j++ {
			if mergeableBorder(pieces[i], pieces[j], nil) <= mergeTolerance {
				continue
			}
			if merged := joinFaces(pieces[i], pieces[j]); len(merged) >= 3 {
				pieces[i] = merged
				pieces = append(pieces[:j], pieces[j+1:]...)
				j = i // retry the enlarged face against all remaining pieces
			}
		}
	}
	return pieces
}

func (cut rockCut) geometry(id int64, old *terrainGeometry, tolerance float64) (*terrainGeometry, []int, bool) {
	if old == nil {
		return old, nil, false
	}
	grid, parents, changed := cut.grid(old.grid, old.top)
	if !changed {
		return old, nil, false
	}
	cuts := append(append([]rockCut(nil), old.cuts...), cut)
	topology := carvedTopology(grid, cuts, old.top)
	updated := prepareTerrainGeometry(sectionData{id: id, guides: old.guides, foregroundTopology: topology}, tolerance)
	updated.vegetation = old.vegetation
	return updated, parents, true
}

func (cut rockCut) grid(source RockGrid, top float64) (RockGrid, []int, bool) {
	var grid RockGrid
	var parents []int
	changed := false
	hole := cut.localPolygon(top)
	for i, cell := range source {
		pieces := [][]V{cell.Polygon}
		if cut.intersects(cell.Polygon, top) {
			pieces = subtractRockCut(cell.Polygon, hole)
			changed = true
		}
		for _, poly := range pieces {
			fragment := cell
			fragment.Polygon = poly
			if !insideFace(fragment.Center, poly) {
				fragment.Center = faceCenter(poly)
				fragment.Z = cell.depthAt(fragment.Center)
			}
			grid = append(grid, fragment)
			parents = append(parents, i)
		}
	}
	return grid, parents, changed
}

func carvedTopology(grid RockGrid, cuts []rockCut, top float64) *rockTopology {
	t := &rockTopology{grid: grid, neighbors: rockNeighbors(grid), cuts: cuts, top: top}
	t.boundary = rockBoundaryEdges(grid, t.neighbors, false)
	return t
}

func (g *Scene) redrawCarvedSection(section *worldSection) {
	if section.foreground == nil { // CPU-only geometry consumers/tests
		return
	}
	mesh := prepareGridWithTopology(nil, g.view, carvedTopology(section.geometry.grid, section.geometry.cuts, section.geometry.top))
	if section.mesh != nil {
		section.mesh.foreground = mesh
		section.mesh.geometry = section.geometry
	}
	g.drawCarvedForeground(section.foreground, mesh, section.geometry.top)
}

func (g *Scene) drawCarvedForeground(dst *ebiten.Image, mesh gridMesh, top float64) {
	pixels := dst.Bounds().Dx()
	mesh = scaleGridMesh(mesh, pixels)
	dst.Clear()
	g.drawGridFaces(dst, mesh.faces, top-SectionHeight)
	if g.world.white == nil {
		g.world.white = ebiten.NewImage(1, 1)
		g.world.white.Fill(color.White)
	}
	for _, outline := range mesh.outlines {
		outline.draw(dst, g.world.white)
	}
}

func (g *Scene) applyStoredCuts(mesh *sectionMesh) {
	changed := false
	plantsChanged := false
	for _, cut := range g.world.cuts {
		geometry, _, edited := cut.geometry(mesh.id, mesh.geometry, g.collisionTolerance)
		geometry, plantsEdited := cut.plants(geometry)
		mesh.geometry = geometry
		changed = changed || edited
		plantsChanged = plantsChanged || plantsEdited
	}
	if changed {
		mesh.foreground = prepareGridWithTopology(nil, g.view, carvedTopology(mesh.geometry.grid, mesh.geometry.cuts, mesh.geometry.top))
	}
	if plantsChanged {
		g.prepareCarvedVegetation(mesh)
	}
}

// Restart affected uploads, including partially drawn or blurred vegetation.
// Published foreground was edited above and need not be uploaded again.
func (g *Scene) carveUpload(cut rockCut) {
	u := g.world.upload
	if u == nil {
		return
	}
	geometry, _, changed := cut.geometry(u.data.id, u.data.geometry, g.collisionTolerance)
	geometry, plantsChanged := cut.plants(geometry)
	if !changed && !plantsChanged {
		return
	}
	defer func() {
		pixels := u.pixels
		if pixels == 0 {
			pixels = g.world.renderWidth()
		}
		u.raster = scaleSectionMesh(u.data, pixels)
	}()
	u.data.geometry = geometry
	if changed {
		u.data.foreground = prepareGridWithTopology(nil, g.view, carvedTopology(geometry.grid, geometry.cuts, geometry.top))
	}
	if plantsChanged {
		g.prepareCarvedVegetation(&u.data)
		for _, img := range []*ebiten.Image{u.vines, u.foregroundVines, u.mushrooms} {
			if img != nil {
				img.Deallocate()
			}
		}
		u.vines, u.foregroundVines, u.mushrooms = nil, nil, nil
		if u.stage >= 5 {
			u.stage, u.next = 5, 0
			if section := g.world.sections[u.data.id]; section != nil {
				section.geometry = geometry
			}
		}
	}
	if u.stage >= 5 {
		return
	}
	if !changed {
		return
	}
	if u.stage >= 2 {
		if u.foreground != nil {
			u.foreground.Deallocate()
			u.foreground = nil
		}
		u.stage, u.next = 2, 0
	}
}
