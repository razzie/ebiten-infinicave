package infinicave

import (
	"github.com/razzie/ebiten-infinicave/internal/terrain"
)

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
type Section = terrain.Section

// SectionContent supplies authored content for a square cave section. Fields
// use section-local scene units, with the owned square spanning (0, 0) to (1, 1).
// The structure can grow to include other authored objects in future versions.
// Empty Guides produces no procedural foreground. Holes remove foreground rock
// after shaping. Hole bounds must fit -1 to 2 along the scrolling axis,
// including seam crossings (Y for Vertical, X for Horizontal);
// declare longer cuts as multiple section-local holes. Invalid holes are ignored.
type SectionContent = terrain.SectionContent

// SectionLoader loads content for one square section. IDs start at 0 at the
// starting edge and decrease in the direction of growth: -1, -2, ... . Points
// use ordinary section-local X/Y in the configured orientation.
// The loader owns its returned slices;
// generation copies them and recomputes guide S, Min, and Max.
// BrightSign defaults to 1; use -1 to reverse the lit side. A zero Seed receives
// a stable seed based on the world seed, section ID, and guide's slice index.
// Guides with nonfinite coordinates or fewer than two distinct consecutive
// points are ignored. Returning empty content does not request random guides.
//
// Generation also loads neighboring sections for seamless padding. IDs may be
// requested repeatedly and in any order, so return consistent content per ID.
// Scene calls the loader on its background generation worker; synchronous
// generation calls it on the calling goroutine. Reset can overlap an old worker,
// so loaders sharing mutable state must be safe for concurrent calls.
type SectionLoader = terrain.SectionLoader

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
	return terrain.GenerateSection(terrain.GenerationOptions{
		Seed:               config.Seed,
		Orientation:        config.Orientation,
		CollisionTolerance: config.CollisionTolerance,
		LoadSection:        config.LoadSection,
		OnCollisionReady:   config.OnCollisionReady,
	}, id)
}
