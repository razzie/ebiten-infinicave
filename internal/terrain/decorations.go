package terrain

import (
	"github.com/razzie/ebiten-infinicave/internal/geom"
)

// Retain original CPU plants for section reuploads. Vines are cut only in the
// finished textures; support damage removes complete mushroom groups.
// Slices are immutable once published; edits create replacements.
type Vegetation struct {
	Vines, ForegroundVines []Vine
	Mushrooms              []MushroomGroup
}

func SectionVegetation(data SectionData) Vegetation {
	return Vegetation{data.Vines, data.ForegroundVines, data.Mushrooms}
}

func (cut RockCut) vegetation(source Vegetation, top float64) (Vegetation, bool) {
	updated := source
	var removed bool
	updated.Mushrooms, removed = cut.mushrooms(source.Mushrooms, top)
	return updated, removed
}

// Damage to an inset support removes the complete attached group.
// A bounds miss allocates nothing and retains the immutable source slice.
func (cut RockCut) mushrooms(source []MushroomGroup, top float64) ([]MushroomGroup, bool) {
	var groups []MushroomGroup
	changed := false
	for i, g := range source {
		hit := false
		for _, m := range g.Mushrooms {
			root := MushroomSupport(m)
			root.Y += top
			if cut.ContainsAnchor(root) {
				hit = true
				break
			}
		}
		if hit {
			if !changed {
				groups = append([]MushroomGroup(nil), source[:i]...)
				changed = true
			}
		} else if changed {
			groups = append(groups, g)
		}
	}
	if !changed {
		return source, false
	}
	return groups, true
}

func (cut RockCut) Plants(old *Geometry) (*Geometry, bool) {
	updated, changed, _ := cut.PlantsWithChanges(old)
	return updated, changed
}

// DecorationChanges identifies whole objects removed by support damage.
// Render recipes consume these IDs without checking the same anchors again.
type DecorationChanges struct {
	Mushrooms, Webs [][2]int64
}

type decorationAnchor struct {
	point  geom.V
	object int
}

// The original objects and index remain immutable across deletions. Each edit
// copies liveness only after a hit; public CPU lists are materialized on change.
type decorationState struct {
	index                       *geom.BoundsIndex
	anchors                     []decorationAnchor
	mushrooms, currentMushrooms []MushroomGroup
	webs, currentWebs           []SpiderWeb
	dead                        []bool
}

func newDecorationState(mushrooms []MushroomGroup, webs []SpiderWeb) *decorationState {
	if len(mushrooms)+len(webs) == 0 {
		return nil
	}
	s := &decorationState{mushrooms: mushrooms, currentMushrooms: mushrooms, webs: webs, currentWebs: webs, dead: make([]bool, len(mushrooms)+len(webs))}
	var bounds []geom.Bounds
	add := func(p geom.V, object int) {
		s.anchors = append(s.anchors, decorationAnchor{p, object})
		bounds = append(bounds, geom.Bounds{Min: p, Max: p})
	}
	for i, group := range mushrooms {
		for _, m := range group.Mushrooms {
			add(MushroomSupport(m), i)
		}
	}
	for i, web := range webs {
		for _, p := range SpiderWebSupports(web) {
			add(p, len(mushrooms)+i)
		}
	}
	s.index = geom.NewBoundsIndex(bounds)
	return s
}

func (s *decorationState) matches(mushrooms []MushroomGroup, webs []SpiderWeb) bool {
	return s != nil && len(mushrooms) == len(s.currentMushrooms) && len(webs) == len(s.currentWebs) &&
		(len(mushrooms) == 0 || &mushrooms[0] == &s.currentMushrooms[0]) &&
		(len(webs) == 0 || &webs[0] == &s.currentWebs[0])
}

func (cut RockCut) PlantsWithChanges(old *Geometry) (*Geometry, bool, DecorationChanges) {
	if old == nil || cut.Max.Y <= old.Top+GenerationMinY || cut.Min.Y >= old.Top+generationMaxY || cut.Max.X <= 0 || cut.Min.X >= Width {
		return old, false, DecorationChanges{}
	}
	if len(old.Vegetation.Mushrooms)+len(old.Topology.SpiderWebs) == 0 {
		return old, false, DecorationChanges{}
	}
	source := old.decorations
	if !source.matches(old.Vegetation.Mushrooms, old.Topology.SpiderWebs) {
		// Legacy/manual geometry can attach plants after preparing rock. The
		// worker path prepares the index before publication instead.
		source = newDecorationState(old.Vegetation.Mushrooms, old.Topology.SpiderWebs)
	}
	var changes DecorationChanges
	var state *decorationState
	source.index.Query(geom.Bounds{Min: cut.Min.Sub(geom.V{Y: old.Top}), Max: cut.Max.Sub(geom.V{Y: old.Top})}, func(i int) bool {
		a := source.anchors[i]
		live := source
		if state != nil {
			live = state
		}
		if live.dead[a.object] {
			return true
		}
		p := a.point.Add(geom.V{Y: old.Top})
		if !cut.ContainsAnchor(p) {
			return true
		}
		if state == nil {
			copy := *source
			copy.dead = append([]bool(nil), source.dead...)
			state = &copy
		}
		state.dead[a.object] = true
		if a.object < len(state.mushrooms) {
			p = state.mushrooms[a.object].Mushrooms[0].Anchor
			changes.Mushrooms = append(changes.Mushrooms, CollisionVertexKey(p.Add(geom.V{Y: old.Top})))
		} else {
			p = state.webs[a.object-len(state.mushrooms)].Corner
			changes.Webs = append(changes.Webs, CollisionVertexKey(p.Add(geom.V{Y: old.Top})))
		}
		return true
	})
	if state == nil {
		return old, false, changes
	}
	updated := *old
	if len(changes.Mushrooms) > 0 {
		var kept []MushroomGroup
		for i, group := range state.mushrooms {
			if !state.dead[i] {
				kept = append(kept, group)
			}
		}
		updated.Vegetation.Mushrooms, state.currentMushrooms = kept, kept
	}
	if len(changes.Webs) > 0 {
		var kept []SpiderWeb
		for i, web := range state.webs {
			if !state.dead[len(state.mushrooms)+i] {
				kept = append(kept, web)
			}
		}
		topology := *old.Topology
		topology.SpiderWebs, state.currentWebs = kept, kept
		updated.Topology = &topology
	}
	updated.decorations = state
	return &updated, true, changes
}

const supportInset = .0005

// InsetRockAnchor finds a point strictly inside the supporting polygon without
// changing visual placement. Shrinking the inset handles thin and jagged rock.
// Only nearby edges can support a placement outside the polygon.
func InsetRockAnchor(poly []geom.V, p, inward geom.V) (geom.V, bool) {
	if len(poly) < 3 {
		return p, false
	}
	inward = inward.Norm()
	if inward.Len2() > 0 {
		for depth := supportInset; depth >= 1e-8; depth *= .5 {
			q := p.Add(inward.Mul(depth))
			if geom.InsidePolygon(q, poly) {
				return q, true
			}
		}
	}
	if geom.InsidePolygon(p, poly) {
		return p, true
	}
	best, edge := .003*.003, -1
	var nearest geom.V
	for i, a := range poly {
		d := poly[(i+1)%len(poly)].Sub(a)
		if d.Len2() == 0 {
			continue
		}
		q := a.Add(d.Mul(geom.Clamp(p.Sub(a).Dot(d)/d.Len2(), 0, 1)))
		if distance := p.Sub(q).Len2(); distance < best {
			best, edge, nearest = distance, i, q
		}
	}
	if edge >= 0 {
		n := poly[(edge+1)%len(poly)].Sub(poly[edge]).Norm().Perp()
		if geom.PolygonArea(poly) < 0 {
			n = n.Mul(-1)
		}
		// At a convex corner, move slightly along the supporting edge as well.
		mid := geom.LerpVector(poly[edge], poly[(edge+1)%len(poly)], .5)
		for depth := supportInset; depth >= 1e-8; depth *= .5 {
			for _, q := range [2]geom.V{nearest.Add(n.Mul(depth)), nearest.Add(n.Add(mid.Sub(nearest).Norm()).Mul(depth))} {
				if geom.InsidePolygon(q, poly) {
					return q, true
				}
			}
		}
	}
	return p, false
}

// MushroomSupport is independent of the visual guide root. Generated plants
// retain a verified rock-interior point; hand-authored plants use their root.
func MushroomSupport(m Mushroom) geom.V {
	if m.supportSet {
		return m.support
	}
	return m.Anchor
}

// SpiderWebSupports excludes all free silk nodes. Generation retains inset
// support points separately from the fixed nodes used to render the network.
func SpiderWebSupports(w SpiderWeb) []geom.V {
	if len(w.supports) > 0 {
		return w.supports
	}
	if w.Network != nil {
		return w.Network.Nodes[:w.Network.Anchors]
	}
	return []geom.V{w.Corner, w.A, w.B}
}
