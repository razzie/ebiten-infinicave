package terrain

import "github.com/razzie/ebiten-infinicave/internal/geom"

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
