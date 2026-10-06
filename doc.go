// Package infinicave generates and renders an infinite procedural cavern with
// Ebitengine. Use Scene in your own game loop, or GenerateSection to work with
// deterministic rock, guide, vine, and mushroom geometry. Foreground collision
// boundaries are always prepared, with optional simplification via Config.
//
// The cave is Width scene units wide, with square sections spanning (0, 0) to
// (1, 1) locally, and grows upward from Y = 0 into
// negative world coordinates. Rendering uses streamed sections with bounded
// caching and asynchronous CPU generation. Input, camera movement, window
// configuration, UI, and export are owned by the application.
package infinicave
