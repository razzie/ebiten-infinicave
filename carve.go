package infinicave

import (
	"fmt"
	"maps"
	"math"
	"sort"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

// Hole describes a foreground rock cut in scene units. Circle uses Center and
// Radius; Segment uses Start, End and full Width. Only the selected fields are
// read. Carve uses world coordinates; SectionContent uses section-local points.
// Radii and widths must be positive and finite; segment endpoints distinct.
type Hole = terrain.Hole

// HoleShape selects a circular blast or a flat-ended rectangular drill cut.
type HoleShape = terrain.HoleShape

const HoleCircle = terrain.HoleCircle

const HoleSegment = terrain.HoleSegment

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
// every affected formation to be loaded (CarveTerrain checks cut coverage only).
// Cuts also apply to future section loads, whose changes cannot be included in
// this immediate result.
type CarveResult struct {
	Changes    []FormationChange
	SectionIDs []int64
	Complete   bool
}

// CarveCircle removes foreground rock in a blast centered at a world point.
// Radius must be finite and positive. The circle uses a 96-sided inscribed
// polygon (maximum radial error about 0.00054 * radius). Rendering, exact
// queries and collision use the same cut. Mushroom groups and webs are removed
// when a rock support anchor is cut. Foreground vines and moss are erased from
// their textures; their original geometry is retained. Background rock and
// background vines are retained.
// Edits persist until Reset or Close, including across eviction.
// Completely destroyed formations release their guides once no supported
// fragment or uncached continuation remains. Reloads skip those guide sources
// and their procedural branches.
// Collision, exact queries, and the result are ready when the call returns.
// Texture edits run on the next valid Update or Draw, before rendering.
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
	return g.carveHole(hole, true)
}

// CarveTerrain applies the same persistent cut and immediate collision updates
// as Carve, without building formation split reports. Changes is always empty;
// Complete describes loaded cut coverage only. A geometry change expires all
// retained formation and guide IDs; a later Query rebuilds them lazily.
// Source guides are retained rather than retired when their last fragment is
// destroyed. Rendering and decoration removal use the usual carving path.
// Use this for gameplay that consumes SectionIDs and owns its collision queries.
func (g *Scene) CarveTerrain(hole Hole) (CarveResult, error) {
	return g.carveHole(hole, false)
}

func (g *Scene) carveHole(hole Hole, reportFormations bool) (CarveResult, error) {
	hole = terrain.MapHole(hole, func(point geom.V) geom.V {
		return terrain.InternalPoint(g.orientation, point)
	})
	cut, err := terrain.CutFromHole(hole)
	if err != nil {
		return CarveResult{}, err
	}
	return g.carve(cut, hole, reportFormations)
}

func (g *Scene) carve(cut terrain.RockCut, hole Hole, reportFormations bool) (CarveResult, error) {
	if g.closed || g.world == nil {
		return CarveResult{}, fmt.Errorf("infinicave: cannot carve a closed scene")
	}
	if cut.Max.X <= 0 || cut.Min.X >= Width || cut.Min.Y >= 0 {
		return CarveResult{Complete: true}, nil
	}
	w := g.world
	// Gameplay needs section coverage, not connected formation identities or
	// their exact union boundaries. Avoid both eager query-index rebuilds.
	q := &worldQueryIndex{}
	if reportFormations {
		q = w.queryIndex()
	} else {
		for id, section := range w.sections {
			if section.geometry != nil {
				q.sections = append(q.sections, id)
			}
		}
		sort.Slice(q.sections, func(i, j int) bool { return q.sections[i] < q.sections[j] })
	}
	result := CarveResult{Complete: cut.Covered(q.sections)}
	if !w.appendCut(hole, cut) {
		return result, nil
	}
	affected := make(map[uint64]bool)
	images := carveImageEdit{cut: cut}
	// Track each replacement face's origin, including unchanged faces from
	// the same formation. This maps both sides of a split to their parent.
	origins := make(map[[2]int64]uint64)
	for _, id := range q.sections {
		section := w.sections[id]
		old := section.geometry
		updated, parents, changed := cut.Geometry(id, old, g.collisionTolerance)
		updated, plantsChanged, removals := cut.PlantsWithChanges(updated)
		section.geometry = updated
		if collision := w.collision[id]; terrain.SameTerrain(old, collision) {
			w.collision[id] = updated
		}
		if section.mesh != nil {
			images.sections = append(images.sections, carveSectionImageEdit{
				id: id, section: section, geometry: updated, removals: removals,
			})
		}
		if !reportFormations {
			if changed {
				result.SectionIDs = append(result.SectionIDs, -id)
			}
			continue
		}
		if !changed {
			if plantsChanged {
				section.geometry = updated
			}
			for _, face := range old.Faces {
				origins[face.Key] = q.faceIDs[face.Key]
			}
			continue
		}
		for i, parent := range parents {
			origins[updated.Faces[i].Key] = q.faceIDs[old.Faces[parent].Key]
		}
		// Only actual removed area marks a formation affected, not a bounds
		// overlap or a boundary touch.
		counts := make([]int, len(old.Faces))
		unchanged := make([]bool, len(old.Faces))
		for i, parent := range parents {
			counts[parent]++
			unchanged[parent] = len(updated.Faces[i].Poly) > 0 && &updated.Faces[i].Poly[0] == &old.Faces[parent].Poly[0] && len(updated.Faces[i].Holes) == len(old.Faces[parent].Holes)
		}
		for i := range old.Faces {
			if counts[i] != 1 || !unchanged[i] {
				affected[q.faceIDs[old.Faces[i].Key]] = true
			}
		}
		section.geometry = updated
		result.SectionIDs = append(result.SectionIDs, -id)
	}
	delete(affected, 0) // padding-only geometry has no query ID
	before := make(map[uint64]Formation)
	sources := make(map[uint64][][32]byte)
	for id := range affected {
		before[id] = q.formations[id]
		for _, block := range q.formationBlocks[id] {
			for _, face := range block.faces {
				sources[id] = append(sources[id], block.geometry.FaceGuideKeys(face)...)
			}
		}
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
	// Collision may be ready while its section is still generating decoration
	// or uploading. Keep that early cache in sync with edits as well.
	for id, geometry := range w.collision {
		if section := w.sections[id]; section != nil && terrain.SameTerrain(section.geometry, geometry) {
			continue
		}
		updated, _, changed := cut.Geometry(id, geometry, g.collisionTolerance)
		w.collision[id] = updated
		if changed && (w.sections[id] == nil || w.sections[id].geometry == nil) {
			result.SectionIDs = append(result.SectionIDs, -id)
		}
	}
	sort.Slice(result.SectionIDs, func(i, j int) bool { return result.SectionIDs[i] > result.SectionIDs[j] })
	if len(images.sections) > 0 || w.upload != nil || len(w.parked) > 0 {
		w.carveImages = append(w.carveImages, images)
	}
	if len(result.SectionIDs) > 0 {
		w.revision++
		if !reportFormations {
			// Discard identity metadata as well as boundaries: preserving old
			// aliases could reconnect pieces separated by this cut.
			w.queries = nil
		}
	}
	if !reportFormations {
		return result, nil
	}
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
	retired := make(map[[32]byte]bool)
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
		if change.Complete && len(change.Remaining) == 0 {
			for _, key := range sources[id] {
				retired[key] = true
			}
		}
	}
	w.retireGuides(retired)
	return result, nil
}

// Complete formations nominate their source guides for retirement. A guide
// shared by another formation, split fragment, or padded continuation stays
// alive until the last supporting shape disappears.
func (w *world) retireGuides(candidates map[[32]byte]bool) {
	if len(candidates) == 0 {
		return
	}
	keep := func(h *terrain.Geometry) {
		if h == nil {
			return
		}
		for face := range h.Faces {
			for _, key := range h.FaceGuideKeys(face) {
				delete(candidates, key)
			}
		}
	}
	for _, section := range w.sections {
		keep(section.geometry)
	}
	for _, h := range w.collision {
		keep(h)
	}
	if len(candidates) == 0 {
		return
	}
	// Jobs retain the old map, so publish a new snapshot instead of mutating
	// data that a generation worker may currently be reading.
	retired := maps.Clone(w.retiredGuides)
	if retired == nil {
		retired = make(map[[32]byte]bool)
	}
	maps.Copy(retired, candidates)
	w.retiredGuides = retired
	for _, section := range w.sections {
		section.geometry = section.geometry.WithoutGuides(retired)
		if section.mesh != nil {
			mesh := *section.mesh
			mesh.Geometry = mesh.Geometry.WithoutGuides(retired)
			section.mesh = &mesh
		}
	}
	for id, h := range w.collision {
		w.collision[id] = h.WithoutGuides(retired)
	}
	for id, mesh := range w.pending {
		mesh.Geometry = mesh.Geometry.WithoutGuides(retired)
		w.pending[id] = mesh
	}
	if w.upload != nil {
		w.upload.data.Geometry = w.upload.data.Geometry.WithoutGuides(retired)
		w.upload.raster.Geometry = w.upload.raster.Geometry.WithoutGuides(retired)
	}
	for _, upload := range w.parked {
		upload.data.Geometry = upload.data.Geometry.WithoutGuides(retired)
		upload.raster.Geometry = upload.raster.Geometry.WithoutGuides(retired)
	}
	w.revision++
	w.queryIndex() // release guide point copies and old formation source snapshots
}

// Commands retain validated input and bounds, not expanded 96-vertex circles.
type cutCommand struct {
	hole     terrain.Hole
	min, max geom.V
}

func (c cutCommand) expand() terrain.RockCut { cut, _ := terrain.CutFromHole(c.hole); return cut }

func (w *world) appendCut(hole terrain.Hole, cut terrain.RockCut) bool {
	// Ignore unselected fields, including callers' non-finite unused values.
	if hole.Shape == HoleCircle {
		hole = Hole{Shape: HoleCircle, Center: hole.Center, Radius: hole.Radius}
	} else {
		hole = Hole{Shape: HoleSegment, Start: hole.Start, End: hole.End, Width: hole.Width}
	}
	if w.cutKeys == nil {
		w.cutKeys = make(map[Hole]bool)
	}
	if w.cutKeys[hole] {
		return false
	}
	w.cutKeys[hole] = true
	w.cuts = append(w.cuts, cutCommand{hole: hole, min: cut.Min, max: cut.Max})
	return true
}

func (w *world) cutsForSection(id int64, since uint64) []int {
	if since >= uint64(len(w.cuts)) {
		return nil
	}
	if w.cutBuckets == nil {
		w.cutBuckets = make(map[int64][]int)
	}
	for w.indexedCuts < len(w.cuts) {
		i := w.indexedCuts
		c := w.cuts[i]
		w.indexedCuts++
		first := max(0, math.Floor(-min(0, c.max.Y)))
		last := max(first, math.Floor(-c.min.Y))
		if last-first > 64 || last > float64(math.MaxInt64-4) {
			w.largeCuts = append(w.largeCuts, i)
			continue
		}
		for bucket := int64(first); bucket <= int64(last); bucket++ {
			w.cutBuckets[bucket] = append(w.cutBuckets[bucket], i)
		}
	}
	var ids []int
	seen := make(map[int]bool)
	add := func(i int) {
		if uint64(i) < since || seen[i] {
			return
		}
		seen[i] = true
		c := w.cuts[i]
		top := terrain.SectionTop(id)
		if c.max.Y > top+terrain.GenerationMinY && c.min.Y < top+2*SectionHeight && c.max.X > 0 && c.min.X < Width {
			ids = append(ids, i)
		}
	}
	for delta := int64(-1); delta <= 1; delta++ {
		if id == math.MaxInt64 && delta == 1 {
			continue
		}
		bucket := id + delta
		if bucket < 0 {
			continue
		}
		for _, i := range w.cutBuckets[bucket] {
			add(i)
		}
	}
	for _, i := range w.largeCuts {
		add(i)
	}
	sort.Ints(ids)
	return ids
}
