package infinicave

import (
	"fmt"
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
// queries and collision use the same cut. Unsupported mushrooms are
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
	hole = terrain.MapHole(hole, func(point geom.V) geom.V {
		return terrain.InternalPoint(g.orientation, point)
	})
	cut, err := terrain.CutFromHole(hole)
	if err != nil {
		return CarveResult{}, err
	}
	return g.carve(cut)
}

func (g *Scene) carve(cut terrain.RockCut) (CarveResult, error) {
	if g.closed || g.world == nil {
		return CarveResult{}, fmt.Errorf("infinicave: cannot carve a closed scene")
	}
	if cut.Max.X <= 0 || cut.Min.X >= Width || cut.Min.Y >= 0 {
		return CarveResult{Complete: true}, nil
	}
	w := g.world
	q := w.queryIndex()
	result := CarveResult{Complete: cut.Covered(q.sections)}
	affected := make(map[uint64]bool)
	// Track each replacement face's origin, including unchanged faces from
	// the same formation. This maps both sides of a split to their parent.
	origins := make(map[[2]int64]uint64)
	for _, id := range q.sections {
		section := w.sections[id]
		old := section.geometry
		updated, parents, changed := cut.Geometry(id, old, g.collisionTolerance)
		updated, plantsChanged := cut.Plants(updated)
		if plantsChanged {
			section.geometry = updated
			g.redrawCarvedVegetation(section, id)
		}
		if !changed {
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
		for i, face := range old.Faces {
			if cut.Intersects(face.Poly, old.Top) {
				affected[q.faceIDs[old.Faces[i].Key]] = true
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
	// Collision may be ready while its section is still generating decoration
	// or uploading. Keep that early cache in sync with edits as well.
	for id, geometry := range w.collision {
		updated, _, changed := cut.Geometry(id, geometry, g.collisionTolerance)
		w.collision[id] = updated
		if changed && w.sections[id] == nil {
			result.SectionIDs = append(result.SectionIDs, -id)
		}
	}
	sort.Slice(result.SectionIDs, func(i, j int) bool { return result.SectionIDs[i] > result.SectionIDs[j] })
	w.cuts = append(w.cuts, cut)
	g.carveUpload(cut)
	if len(result.SectionIDs) > 0 {
		w.revision++
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
