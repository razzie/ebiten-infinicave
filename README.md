# ebiten-infinicave

A Go library for an infinite procedural cavern with faceted rock, branching vines, and mushrooms, rendered with Ebitengine. The world starts at a bottom edge and grows infinitely upward as you scroll. Terrain is generated in the background and nearby sections are cached; returning to an evicted area regenerates the same rocks and vines from its seed.

The root package is `infinicave`; the interactive viewer and PNG exporter live in
`cmd/infinicave`. The library leaves your game loop, input, camera, window settings,
and UI under your control.

Import it as `github.com/razzie/ebiten-infinicave`:

```sh
go get github.com/razzie/ebiten-infinicave
```

Create a scene once and close it when your game exits:

```go
config := infinicave.DefaultConfig()
config.Seed = 42
scene, err := infinicave.NewScene(config)
if err != nil {
    return err
}
defer scene.Close()
```

Keep `scene` in your own `ebiten.Game`. In `Update`, advance your camera and call
`scene.Update(viewport)` once per tick. In `Draw`, call `scene.Draw(screen, viewport)`.
For example, a stationary view of the bottom 800 pixels uses:

```go
viewport := infinicave.Viewport{Y: -800, Height: 800}
ready := scene.Update(viewport) // false while required sections are loading
scene.Draw(screen, viewport)
// Add your own loading UI when !ready.
```

The cave has a fixed logical width of `infinicave.Width` (1000 pixels). Set your
`Layout` to that width and the viewport height. World Y is negative above the
floor at zero, so the initial viewport Y is `-float64(height)` and scrolling
upward decreases it. Set `Viewport.Velocity` to camera movement in pixels per tick
for directional prefetching. Y must be finite and no greater than `-float64(height)`;
height must be positive. Invalid viewports do no work and report not ready.

`scene.DrawHover(screen, viewport, x, y)` adds an optional highlight after drawing;
pass cursor coordinates in logical viewport pixels and call it only when the
cursor is active. `scene.Reset(seed)` discards cached sections and starts a new
world with the same rendering settings. Rendering does not include UI or exports.
Call every `Scene` method on the Ebitengine game goroutine. `Close` is idempotent
and releases shaders and cached images; an in-progress CPU generation finishes
before its worker exits.

`DefaultConfig` uses texture strength 8, shaded rendering, seed 0, and exact
collision geometry. A zero `Config` is also valid and disables texture. `Study`
and `View` are typed enums:

```go
config.Study = infinicave.StudyNone // or StudyLedge, StudyCurl
config.View = infinicave.ViewShaded // or ViewClay, ViewHeight, ViewNormals, ViewShadows
config.CollisionTolerance = 2 // approximation tolerance in world pixels
```

`Texture` accepts 0–16. Diagnostic views disable texture and hide vegetation.
`ParseStudy` and `ParseView` convert strings such as `"curl"` and `"normals"` for
command-line tools; library code can use the constants directly.

Foreground geometry is always prepared and retained, including in diagnostic
views. Calling `DrawHover` is optional and has no effect on collision availability.
`CollisionTolerance` must be finite and nonnegative. Zero preserves exact rock
boundaries; larger values simplify collision polygons to reduce vertex counts
and collision cost while the rendered terrain stays detailed. For example, 2
allows up to 2 world pixels of original-vertex deviation from a simplified edge.
Simplification keeps section crossings fixed and rejects invalid loops or changes
that break a block's hole topology. Shapes that cannot safely simplify retain
exact boundaries, so the tolerance does not guarantee a particular vertex count.

After `Update`, obtain collision geometry for a loaded section:

```go
geometry, ready := scene.CollisionGeometry(0)
if ready {
    solid := geometry.Contains(infinicave.V{X: 500, Y: -400})
    // Use solid for a point query, or build physics fixtures from geometry.Polygons.
    _ = solid
}
```

The returned geometry is an independent copy; cache it when building your physics
world rather than requesting a fresh copy every tick. Missing or evicted sections
return `false`. Its polygons describe the union of foreground rock, with internal
cell edges removed. Loops may be concave; outer loops have positive signed area
and holes have negative signed area. Physics engines requiring convex fixtures
need decomposition that respects holes. Background rock, vines, mushrooms, and
decorative bevels are not included as solid geometry.

For access to terrain data without creating a renderer:

```go
section, err := infinicave.GenerateSection(42, 0)
if err != nil {
    return err
}
// section.Foreground contains rock face polygons, heights, and normals.
// Convert local Y to world Y by adding section.WindowTop.
// section.Collision.Polygons already use world coordinates.
```

`GenerateSection` is synchronous and allocates no GPU resources. Run it in a
background goroutine in an interactive application. IDs start at 0 at the bottom
and increase upward; negative IDs return an error. Each section owns its geometry
and includes one section of padding above and below for seamless generation.
Its owned world band is `[section.Top, section.Top + infinicave.SectionHeight)`;
vegetation may extend outside that band. Collision contours also include padding;
use only the owned band when combining adjacent sections. The exposed grids
describe visual faces, and `section.Collision` contains the collision boundaries.
Use `GenerateSectionWithConfig(config, id)` to share a scene's study and collision
tolerance with synchronous generation. This
package depends on Ebitengine, so desktop initialization still needs a graphical
environment even when only generating geometry.

The API is an initial foundation and may change as gameplay requirements develop.

Background vines use muted colors, a maximum stem width of 9 pixels, fine world-aligned grain, and a subtle one-pixel blur to sit behind the raised rock and mushrooms. Grain follows the `-texture` setting; the softened silhouette is baked into each cached vine layer.

Sparse foreground vines grow directly across the raised rock faces, with at most two trunks per section and a few attached branches. These stems use dusty rust and rose colors, subdued highlights, small contact shadows, and seam-following tendrils. Growth stays within the visible rock surface, including shaded faces, and leaves at least 30 pixels between the vine ribbons and guide lines to keep the crests clear. Full branches remain continuous across cached section boundaries.

Tiny offshoots attach to the larger vines and trace the actual background cell edges through their junctions. Every other completed offshoot is retained for a lighter density. These slender branches taper over short lengths, inherit their parent’s subdued material, and respect the foreground rock boundaries.

Each section grows new random guide curves with varying lengths, directions, and bends. Placement favors underfilled areas while reserving a few open pockets. The finished jagged guides stay at least 155 pixels apart, including across section boundaries, and tight folds or self-crossings are rejected. Coverage aims for about two-thirds of the scene within 115 pixels of a guide, leaving gaps for platform-game layouts. Seeds reproduce the same scene, but different seeds and sections use fresh shapes rather than fixed curve templates.

Guides form slightly jagged rock borders by cutting the foreground Voronoi cells along broad, uneven facets. Tiny clipping fragments and long, thin foreground cells merge into neighbors on the same side of the guide, favoring compact combined faces. Site spacing compresses across exposed lips and expands into the flanks without adding aligned rows. Flanks use fewer, larger facets; the outer footprint is cut through the cells along a relief contour, rather than ending in a row of whole tiles.

Guides define solid raised formations: a narrow bevel rises to a crest, then a broad flank descends toward the recessed background. The flank steepens toward its foot so the surface turns gradually into shadow. Control-point heights follow this shared relief with small mineral irregularities. Neighboring heights determine one normal per face, and guide-adjacent faces turn toward the guide. A single light from above and left illuminates the rock; a world-aligned depth buffer supplies cast shadows and contact darkening. Existing rock faces stay opaque even in shadow. Exposed boundaries receive shallow side walls and narrow bevels, while internal triangulation remains invisible. A restrained warm-gray palette reserves the lightest tones for crests, with coherent patina on the lower spurs and faint mineral strokes aligned with each facet. Side walls and bevels remain shallow. Background rock retains a darker charcoal material.

Run with `go run ./cmd/infinicave` (Go 1.27 and a graphical desktop). Use `-seed 42` for a reproducible world.

- Mouse wheel / trackpad: scroll vertically; scroll up to explore new terrain.
- Up / Down or W / S: scroll continuously.
- Page Up / Page Down: move by most of a viewport.
- Home / End: return to the starting bottom edge.
- R: generate a new world while keeping the current position.

The window is resizable. Scrolling continues while missing sections are prepared in the background, with “Growing upward…” displayed until they are ready. Rocks share world coordinates across sections, and vines keep their full geometry across boundaries.

Hover over a foreground rock for a soft warm highlight and glow over its entire connected block of cells, including across cached section boundaries. Point within 6 pixels of a guide to highlight only that guide line instead. Hover effects are enabled by default; use `-hover=false` to disable them (or `-hover=true` to enable them). PNG exports never include hover effects.

`go run ./cmd/infinicave -seed 42 -output scene.png` exports the bottom 1000 × 2400 pixels and exits. `-texture 0` disables the surface texture. `-collision-tolerance 2` simplifies collision polygons with a 2-pixel tolerance.

For shape studies without vines, use `-study ledge` or `-study curl`. The `-view` options are `shaded` (default), `clay`, `height`, `normals`, and `shadows`. Diagnostic views disable texture and hide vines; clay uses neutral gray material with the same lighting and exposed edges.

```sh
go run ./cmd/infinicave -seed 42 -study ledge -view clay -output ledge.png
go run ./cmd/infinicave -seed 42 -study curl -view clay -output curl.png
go run ./cmd/infinicave -seed 42 -study curl -view height -output height.png
go run ./cmd/infinicave -seed 42 -study curl -view normals -output normals.png
go run ./cmd/infinicave -seed 42 -study curl -view shadows -output shadows.png
```

Relief, shadows, rock triangulation, outlines, and vine and mushroom meshes are prepared in the background once per cached section. Background rock, foreground rock, both vine layers, and mushrooms are polygonized concurrently, using up to `GOMAXPROCS - 1` workers (at least one) to leave CPU capacity for rendering. The game loop uploads the prepared meshes over several ticks, then adds the completed section to the scene with all its layers together. The renderer uses a shallow 2.5D surface: illumination is constant within each polygon, with depth shadows averaged over the face to preserve the faceted appearance.

Run checks with `go test ./...` and `go vet ./...`. Ebitengine initializes the display for tests, so these also need a graphical session.
