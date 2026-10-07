package infinicave

import "math/rand"

// A worker owns its cache. Canonical proposals and resolved guides are never
// translated or handed to callers, so revisits and load order cannot mutate them.
type guideCache struct {
	orientation Orientation
	seed        int64
	proposals   map[int64][]Guide
	resolved    map[int64][]Guide
}

func newGuideCache(seed int64, orientation ...Orientation) *guideCache {
	return &guideCache{orientation: optionalOrientation(orientation), seed: seed, proposals: make(map[int64][]Guide), resolved: make(map[int64][]Guide)}
}

func (c *guideCache) window(id int64) []Guide {
	for owner := id - 3; owner <= id+3; owner++ {
		if _, ok := c.proposals[owner]; !ok {
			c.proposals[owner] = generateGuideSection(rand.New(rand.NewSource(sectionSeed(c.seed, owner))), c.orientation)
		}
	}
	var guides []Guide
	for owner := id + 2; owner >= id-2; owner-- {
		section, ok := c.resolved[owner]
		if !ok {
			section = spacedWorldGuideSection(c.seed, owner, c.proposals, c.orientation)
			for i := range section {
				section[i].Seed = sectionSeed(sectionSeed(c.seed, owner), int64(i+1)) | 1
			}
			c.resolved[owner] = section
		}
		for _, g := range section {
			g = shiftedGuide(g, sectionTop(owner)-sectionTop(id))
			g.S = append([]float64(nil), g.S...)
			guides = append(guides, g)
		}
	}
	// Keep exactly the current window, even after a large camera jump.
	for owner := range c.proposals {
		if owner < id-3 || owner > id+3 {
			delete(c.proposals, owner)
		}
	}
	for owner := range c.resolved {
		if owner < id-2 || owner > id+2 {
			delete(c.resolved, owner)
		}
	}
	return guides
}

type sectionBuilder struct {
	seed        int64
	loadSection SectionLoader
	guides      *guideCache
	fields      vineWorkspace
}

func newSectionBuilder(seed int64, loadSection SectionLoader, orientation ...Orientation) *sectionBuilder {
	return &sectionBuilder{seed: seed, loadSection: loadSection, guides: newGuideCache(seed, orientation...)}
}

func (b *sectionBuilder) build(id int64) sectionData {
	return b.buildWithTerrain(id, nil)
}

// onTerrain runs before decoration, once foreground topology includes all
// authored holes. Published terrain is read-only for the rest of the build.
func (b *sectionBuilder) buildWithTerrain(id int64, onTerrain func(sectionData)) sectionData {
	return buildSectionCached(b.seed, id, b.loadSection, b.guides, &b.fields, onTerrain)
}

// Three independent samples per world site need no stateful RNG or seed table.
// Coordinates and channel, rather than section/window order, choose the sample.
func siteRandom(seed, row, col, channel int64) float64 {
	x := sectionSeed(sectionSeed(sectionSeed(seed, row), col), channel)
	return float64(uint64(x)>>11) * (1.0 / (1 << 53))
}
