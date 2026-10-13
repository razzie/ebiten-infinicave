package infinicave

import (
	"math"
	"sort"

	"github.com/razzie/ebiten-infinicave/internal/geom"
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

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
