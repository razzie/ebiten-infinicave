package terrain

import "time"

// GenerationTimings excludes application collision callbacks and GPU work.
// Timers belong to the worker-owned build, not deterministic section data.
type GenerationTimings struct{ Geometry, Material, Decoration time.Duration }

func (s *SectionBuild) Timings() GenerationTimings { return s.timings }
