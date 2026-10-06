package infinicave

// Fixed rock shapes keep collision and diagnostic tests reproducible while
// exercising the same section loader used for authored game terrain.
func testLedgeSection(id int64) SectionContent {
	return testRockSection(id, []V{{0.11, 0.42}, {0.5, 0.365}, {0.89, 0.31}})
}

func testCurlSection(id int64) SectionContent {
	return testRockSection(id, []V{{0.08, 0.65}, {0.38, 0.52}, {0.65, 0.41}, {0.71, 0.26}, {0.57, 0.19}, {0.45, 0.29}, {0.52, 0.37}})
}

func testRockSection(id int64, knots []V) SectionContent {
	guide := ridgedGuide(splineGuide(knots, 1), 42)
	guide.Seed = sectionSeed(42, id)
	return SectionContent{Guides: []Guide{guide}}
}
