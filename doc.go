// Package infinicave generates and renders an infinite procedural cavern with
// Ebitengine. Use Scene in your own game loop, or GenerateSection to work with
// deterministic rock, guide, vine, and mushroom geometry. Foreground collision
// boundaries are always prepared, with optional simplification via Config.
//
// The cave has a fixed Width scene units across its bounded axis, with square
// sections spanning (0, 0) to (1, 1) locally. Config.Orientation defaults to
// Vertical, growing upward from Y = 0 into negative Y; Horizontal grows
// rightward from X = 0 into positive X. Section IDs are 0, -1, -2, ... in the
// direction of growth. Guides favor horizontal ledges and plants grow upright
// in both orientations. Set
// Config.LoadSection to supply authored guides and holes instead of random layouts.
// Rendering uses streamed sections with bounded
// caching and asynchronous CPU generation. Input, camera movement, window
// configuration, UI, and export are owned by the application.
package infinicave
