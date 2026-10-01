package main

// Repeated in world coordinates so diagnostics exercise the same streaming
// and overlap behavior as the full scene, without branches or vine clutter.
func studyGuides(id int64, study string) []Guide {
	var guides []Guide
	for owner := id + 2; owner >= id-2; owner-- {
		knots := []V{{110, 420}, {500, 365}, {890, 310}}
		if study == "curl" {
			knots = []V{{80, 650}, {380, 520}, {650, 410}, {710, 260}, {570, 190}, {450, 290}, {520, 370}}
		}
		g := ridgedGuide(splineGuide(knots, 1), 42)
		g.translateY(sectionTop(owner) - sectionWindowTop(id))
		guides = append(guides, g)
	}
	return guides
}
