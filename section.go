package infinicave

import "fmt"

// Section holds generated geometry for a section plus one section of padding
// above and below. Geometry uses scene units and section-local coordinates:
// the owned square runs from (0, 0) to (1, 1). Add Top to Y to get world
// coordinates. Padding spans Y = -1 to 2, with WindowTop marking its world
// start. The owned world band is [Top, Top+SectionHeight). Vegetation may extend
// into padding and should be drawn once per owning section.
//
// Each generated Section owns its slices; callers may modify them. Rock grids
// describe visual faces; Collision contains boundaries for physics integration.
type Section struct {
	ID                     int64
	Top, WindowTop         float64
	Background, Foreground RockGrid
	Vines, ForegroundVines []Vine
	Mushrooms              []MushroomGroup
	Guides                 []Guide
	// Collision contains foreground rock boundaries in world coordinates.
	Collision CollisionGeometry
}

// GenerateSection synchronously generates a full cave section without
// allocating GPU resources. ID 0 is the bottom section; larger IDs grow upward.
// The same seed and ID reproduce the same geometry, independently of load order.
// Negative IDs return an error. Generation is expensive; use a background
// goroutine when calling from a game. This package still depends on Ebitengine,
// whose initialization requires a graphical environment on desktop platforms.
func GenerateSection(seed, id int64) (Section, error) {
	return GenerateSectionWithConfig(Config{Seed: seed}, id)
}

// GenerateSectionWithConfig generates a section using the same seed, study,
// and collision tolerance as a Scene. Texture and View only affect rendering.
func GenerateSectionWithConfig(config Config, id int64) (Section, error) {
	if err := config.validate(); err != nil {
		return Section{}, err
	}
	if id < 0 {
		return Section{}, fmt.Errorf("infinicave: section ID must be nonnegative")
	}
	data := buildSectionMode(config.Seed, id, config.Study)
	geometry := prepareTerrainGeometry(data, config.CollisionTolerance*generationWidth)
	section := Section{
		ID: data.id, Top: sectionTop(id), WindowTop: sectionWindowTop(id),
		Background: data.background, Foreground: data.foreground,
		Vines: data.vines, ForegroundVines: data.foregroundVines,
		Mushrooms: data.mushrooms, Guides: data.guides,
		Collision: geometry.collision,
	}
	section.normalize()
	return section, nil
}
