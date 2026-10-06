package infinicave

// Repeated in world coordinates so diagnostics exercise the same streaming
// and overlap behavior as the full scene, without branches or vine clutter.
func studyGuides(id int64, study Study) []Guide {
	var guides []Guide
	for owner := id + 2; owner >= id-2; owner-- {
		knots := []V{{0.11, 0.42}, {0.5, 0.365}, {0.89, 0.31}}
		if study == StudyCurl {
			knots = []V{{0.08, 0.65}, {0.38, 0.52}, {0.65, 0.41}, {0.71, 0.26}, {0.57, 0.19}, {0.45, 0.29}, {0.52, 0.37}}
		}
		g := ridgedGuide(splineGuide(knots, 1), 42)
		g.Seed = sectionSeed(42, owner)
		g.translateY(sectionTop(owner) - sectionTop(id))
		guides = append(guides, g)
	}
	return guides
}
