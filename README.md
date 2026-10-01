# Voronoi guides

A procedural rock scene with branching red vines, rendered with Ebitengine. The world starts at a bottom edge and grows infinitely upward as you scroll. Terrain is generated in the background and nearby sections are cached; returning to an evicted area regenerates the same rocks and vines from its seed.

Guides form slightly jagged rock borders by cutting the foreground Voronoi cells along broad, uneven facets. Tiny clipping fragments and long, thin foreground cells merge into neighbors on the same side of the guide, favoring compact combined faces. Site spacing compresses across exposed lips and expands into the flanks without adding aligned rows. Flanks use fewer, larger facets; the outer footprint is cut through the cells along a relief contour, rather than ending in a row of whole tiles.

Guides define solid raised formations: a narrow bevel rises to a crest, then a broad flank descends toward the recessed background. The flank steepens toward its foot so the surface turns gradually into shadow. Control-point heights follow this shared relief with small mineral irregularities. Neighboring heights determine one normal per face, and guide-adjacent faces turn toward the guide. A single light from above and left illuminates the rock; a world-aligned depth buffer supplies cast shadows and contact darkening. Existing rock faces stay opaque even in shadow. Exposed boundaries receive shallow side walls and narrow bevels, while internal triangulation remains invisible. A restrained warm-gray palette reserves the lightest tones for crests, with coherent patina on the lower spurs and faint mineral strokes aligned with each facet. Side walls and bevels remain shallow. Background rock retains a darker charcoal material.

Run with `go run .` (Go 1.27 and a graphical desktop). Use `-seed 42` for a reproducible world.

- Mouse wheel / trackpad: scroll vertically; scroll up to explore new terrain.
- Up / Down or W / S: scroll continuously.
- Page Up / Page Down: move by most of a viewport.
- Home / End: return to the starting bottom edge.
- R: generate a new world while keeping the current position.

The window is resizable. When generation needs to catch up, the view waits at the last loaded position and displays “Growing upward…”. Rocks share world coordinates across sections, and vines keep their full geometry across boundaries.

`go run . -seed 42 -output scene.png` exports the bottom 1000 × 2400 pixels and exits. `-texture 0` disables the surface texture.

For shape studies without vines, use `-study ledge` or `-study curl`. The `-view` options are `shaded` (default), `clay`, `height`, `normals`, and `shadows`. Diagnostic views disable texture and hide vines; clay uses neutral gray material with the same lighting and exposed edges.

```sh
go run . -seed 42 -study ledge -view clay -output ledge.png
go run . -seed 42 -study curl -view clay -output curl.png
go run . -seed 42 -study curl -view height -output height.png
go run . -seed 42 -study curl -view normals -output normals.png
go run . -seed 42 -study curl -view shadows -output shadows.png
```

Relief and shadows are generated once per cached section. The renderer uses a shallow 2.5D surface: illumination is constant within each polygon, with depth shadows averaged over the face to preserve the faceted appearance.

Run checks with `go test ./...` and `go vet ./...`. Ebitengine initializes the display for tests, so these also need a graphical session.
