package terrain

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
	return &guideCache{orientation: OptionalOrientation(orientation), seed: seed, proposals: make(map[int64][]Guide), resolved: make(map[int64][]Guide)}
}

func (c *guideCache) window(id int64) []Guide {
	return c.windowWithoutGuides(id, nil)
}

func (c *guideCache) windowWithoutGuides(id int64, retired map[[32]byte]bool) []Guide {
	for owner := id - 3; owner <= id+3; owner++ {
		if _, ok := c.proposals[owner]; !ok {
			c.proposals[owner] = generateGuideSection(rand.New(rand.NewSource(SectionSeed(c.seed, owner))), c.orientation)
		}
	}
	var guides []Guide
	for owner := id + 2; owner >= id-2; owner-- {
		section, ok := c.resolved[owner]
		if !ok {
			section = spacedWorldGuideSection(c.seed, owner, c.proposals, c.orientation)
			for i := range section {
				section[i].Seed = SectionSeed(SectionSeed(c.seed, owner), int64(i+1)) | 1
			}
		}
		section = liveGuides(section, SectionTop(owner), retired)
		c.resolved[owner] = section
		for _, g := range section {
			g = shiftedGuide(g, SectionTop(owner)-SectionTop(id))
			g.S = append([]float64(nil), g.S...)
			guides = append(guides, g)
		}
	}
	// Keep exactly the current window, even after a large camera jump.
	for owner := range c.proposals {
		if owner < id-3 || owner > id+3 {
			delete(c.proposals, owner)
			continue
		}
		// Resolved guides can share point arrays with their proposals. Drop
		// that owner's proposal cache too; rebuilding the original proposals
		// on demand preserves neighboring placement without retaining dead
		// guide arrays between builds.
		if len(retired) > 0 {
			for _, g := range c.proposals[owner] {
				if retired[GuideKey(g.Pts, SectionTop(owner))] {
					delete(c.proposals, owner)
					break
				}
			}
		}
	}
	for owner := range c.resolved {
		if owner < id-2 || owner > id+2 {
			delete(c.resolved, owner)
		}
	}
	return guides
}

func worldGuides(seed, id int64) []Guide {
	return newGuideCache(seed).window(id)
}
