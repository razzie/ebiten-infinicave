package terrain

import "fmt"

// GenerationOptions contains the settings shared by synchronous generation and
// the scene's worker. Rendering settings are validated by the public facade.
type GenerationOptions struct {
	Seed               int64
	Orientation        Orientation
	CollisionTolerance float64
	LoadSection        SectionLoader
	OnCollisionReady   func(CollisionGeometry)
}

// GenerateSection returns an owned, world-oriented section. The caller validates
// configuration before calling; section IDs are validated here.
func GenerateSection(config GenerationOptions, id int64) (Section, error) {
	if id > 0 || id == -1<<63 {
		return Section{}, fmt.Errorf("infinicave: section ID must be nonpositive and greater than the minimum int64")
	}
	// Streaming uses nonnegative indices internally in the direction of growth.
	index := -id
	var collision CollisionGeometry
	data := NewSectionBuilder(config.Seed, OrientedSectionLoader(config.Orientation, config.LoadSection), config.Orientation).BuildWithTerrain(index, func(data SectionData) {
		collision = OrientedCollision(config.Orientation, PrepareTerrainGeometry(data, config.CollisionTolerance).Collision)
		if config.OnCollisionReady != nil {
			config.OnCollisionReady(CopyCollisionGeometry(collision))
		}
	})
	section := Section{
		ID: id, Top: SectionTop(index), WindowTop: SectionWindowTop(index),
		Background: data.Background, Foreground: data.Foreground,
		Vines: data.Vines, ForegroundVines: data.ForegroundVines,
		Mushrooms: data.Mushrooms, Guides: data.Guides, Holes: data.Holes,
		Collision: collision,
	}
	return OrientedSection(config.Orientation, section), nil
}
