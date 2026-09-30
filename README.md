# Voronoi guides

A procedural rock scene with branching red vines, rendered with Ebitengine. The world starts at a bottom edge and grows infinitely upward as you scroll. Terrain is generated in the background and nearby sections are cached; returning to an evicted area regenerates the same rocks and vines from its seed.

Run with `go run .` (Go 1.27 and a graphical desktop). Use `-seed 42` for a reproducible world.

- Mouse wheel / trackpad: scroll vertically; scroll up to explore new terrain.
- Up / Down or W / S: scroll continuously.
- Page Up / Page Down: move by most of a viewport.
- Home / End: return to the starting bottom edge.
- R: generate a new world while keeping the current position.

The window is resizable. When generation needs to catch up, the view waits at the last loaded position and displays “Growing upward…”. Rocks share world coordinates across sections, and vines keep their full geometry across boundaries.

`go run . -seed 42 -output scene.png` exports the bottom 1000 × 2400 pixels and exits. `-texture 0` disables the surface texture.

Run checks with `go test ./...` and `go vet ./...`. Ebitengine initializes the display for tests, so these also need a graphical session.
