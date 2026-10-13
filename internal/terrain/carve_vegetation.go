package terrain

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
