package infinicave

import "fmt"

// Section holds generated geometry plus one section of padding on each side
// along the scrolling axis. Geometry uses ordinary X/Y scene units, local to
// the owned square [0, 1]². Add Origin to local points to get world coordinates.
// Padding spans local Y = -1 to 2 in Vertical scenes or local X = -1 to 2 in
// Horizontal scenes. Min/Max describe the owned world square; WindowOrigin
// starts the padded window. Vegetation may extend into padding and should be
// drawn once per owning section. Background cells extend from -0.5 to 1.5
// across the bounded axis. Top and WindowTop are legacy Vertical metadata.
//
// Each generated Section owns its slices; callers may modify them. Rock grids
// describe visual faces; Collision contains boundaries for physics integration.
type Section struct {
	Orientation Orientation
	// Origin converts section-local points to world coordinates by addition.
	// Min/Max bound the owned square; WindowOrigin starts the padded window.
	Origin, WindowOrigin, Min, Max V
	// ID is 0 at the starting edge, then -1, -2, ... upward or rightward.
	// Top is ID-1 in Vertical scenes and zero in Horizontal scenes.
	ID                     int64
	Top, WindowTop         float64
	Background, Foreground RockGrid
	Vines, ForegroundVines []Vine
	Mushrooms              []MushroomGroup
	Guides                 []Guide
	Holes                  []Hole
	// Collision contains foreground rock boundaries in world coordinates.
	Collision CollisionGeometry
}

// GenerateSection synchronously generates a full cave section without
// allocating GPU resources. It uses Vertical orientation. ID 0 is the bottom
// section; IDs -1, -2, ... grow upward. Use GenerateSectionWithConfig for Horizontal.
// The same seed and ID reproduce the same geometry, independently of load order.
// Positive IDs return an error. Generation is expensive; use a background
// goroutine when calling from a game. This package still depends on Ebitengine,
// whose initialization requires a graphical environment on desktop platforms.
func GenerateSection(seed, id int64) (Section, error) {
	return GenerateSectionWithConfig(Config{Seed: seed}, id)
}

// GenerateSectionWithConfig generates a section using the same seed,
// collision tolerance, and section content loader as a Scene. Texture,
// background effects, and View only affect rendering. A custom loader must return
// the same content for repeated IDs to preserve geometry across neighboring
// sections and cache eviction.
// OnCollisionReady, when set, runs on the caller's goroutine before vegetation
// generation and receives an independent copy of the section's collision.
func GenerateSectionWithConfig(config Config, id int64) (Section, error) {
	if err := config.validate(); err != nil {
		return Section{}, err
	}
	if id > 0 || id == -1<<63 {
		return Section{}, fmt.Errorf("infinicave: section ID must be nonpositive and greater than the minimum int64")
	}
	// Streaming uses nonnegative indices internally in the direction of growth.
	index := -id
	var collision CollisionGeometry
	data := newSectionBuilder(config.Seed, config.Orientation.sectionLoader(config.LoadSection), config.Orientation).buildWithTerrain(index, func(data sectionData) {
		collision = config.Orientation.collision(prepareTerrainGeometry(data, config.CollisionTolerance).collision)
		if config.OnCollisionReady != nil {
			config.OnCollisionReady(copyCollisionGeometry(collision))
		}
	})
	section := Section{
		ID: id, Top: sectionTop(index), WindowTop: sectionWindowTop(index),
		Background: data.background, Foreground: data.foreground,
		Vines: data.vines, ForegroundVines: data.foregroundVines,
		Mushrooms: data.mushrooms, Guides: data.guides, Holes: data.holes,
		Collision: collision,
	}
	return config.Orientation.section(section), nil
}
