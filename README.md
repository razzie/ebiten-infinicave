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
For example, a stationary view of the bottom 0.8 scene units uses:

```go
viewport := infinicave.Viewport{Y: -0.8, Height: 0.8}
ready := scene.Update(viewport) // false while required sections are loading
scene.Draw(screen, viewport)
// Add your own loading UI when !ready.
```

The cave is `infinicave.Width` (1 scene unit) wide. Each section is a square,
with its owned local area running from `(0, 0)` to `(1, 1)`;
`infinicave.SectionHeight` is also 1. World Y is negative above the floor at zero,
so the initial viewport Y is `-viewport.Height` and scrolling upward decreases it.
Section IDs follow that direction: `0` at the bottom, then `-1`, `-2`, and so on
upward. Section `id` owns the world band from Y = `id-1` to Y = `id`.
`Viewport.Height` is a floating-point height in scene units, and
`Viewport.Velocity` is camera movement in scene units per tick for directional
prefetching. Y must be finite and no greater than `-viewport.Height`; height must
be finite and positive. Invalid viewports do no work and report not ready.

Rendering scales scene units uniformly to the destination image's width. Use a
`Layout` with the same aspect ratio as the viewport: for example, a 1000 × 800
image for a viewport height of 0.8, or a 500 × 500 image for a single unit square.
The generator and cached textures retain their fixed resolution independently
of scene coordinates.

`scene.DrawHover(screen, viewport, x, y)` adds an optional highlight after drawing;
pass cursor coordinates in viewport-local scene units and call it only when the
cursor is active. Convert cursor pixels to scene units with
`float64(infinicave.Width) / float64(screen.Bounds().Dx())`.
`scene.Reset(seed)` discards cached sections and starts a new
world with the same rendering settings and guide loader. Rendering does not include UI or exports.
Call every `Scene` method on the Ebitengine game goroutine. `Close` is idempotent
and releases shaders and cached images; an in-progress CPU generation finishes
before its worker exits.

`DefaultConfig` uses texture strength 8, shaded rendering, seed 0, and exact
collision geometry. A zero `Config` is also valid and disables texture. `Study`
and `View` are typed enums:

```go
config.Study = infinicave.StudyNone // or StudyLedge, StudyCurl
config.View = infinicave.ViewShaded // or ViewClay, ViewHeight, ViewNormals, ViewShadows
config.CollisionTolerance = 0.002 // approximation tolerance in scene units
```

`Texture` accepts 0–16. Diagnostic views disable texture and hide vegetation.
`ParseStudy` and `ParseView` convert strings such as `"curl"` and `"normals"` for
command-line tools; library code can use the constants directly.

Set `Config.LoadGuides` to load your own guide polylines per section:

```go
config.LoadGuides = func(id int64) []infinicave.Guide {
    switch id {
    case 0:
        return []infinicave.Guide{{
            Pts: []infinicave.V{{X: 0.2, Y: 0.4}, {X: 0.7, Y: 0.5}},
        }}
    case -1:
        return []infinicave.Guide{{
            Pts: []infinicave.V{{X: 0.3, Y: 0.3}, {X: 0.8, Y: 0.4}},
        }}
    }
    return nil // this section has no guides
}
```

Assign the callback before calling `NewScene`, or pass the config to
`GenerateSectionWithConfig`. Points use section-local scene units in the owned
unit square. They form a polyline; generation copies the points and computes arc
lengths and bounds, so only `Pts` is required. `BrightSign` defaults to 1; use -1
to reverse the lit side. `Seed` defaults to a stable seed derived from the world
seed, section ID, and guide index. Supplied guides keep their shapes and placement;
custom layouts are responsible for their own spacing.

A nil `LoadGuides` keeps the default random generator or selected study. A
callback returning nil or an empty slice produces no guides for that section.
The callback overrides study guide shapes. Invalid polylines with nonfinite
points or fewer than two distinct consecutive points are ignored. Neighboring
sections are also loaded for seamless generation, and IDs may be requested
repeatedly in any order. Return consistent results per ID. Scene calls the loader
on its background worker; synchronous generation calls it directly. Protect any
shared mutable state against concurrent calls, including during `Reset`.

Foreground geometry is always prepared and retained, including in diagnostic
views. Calling `DrawHover` is optional and has no effect on collision availability.
`CollisionTolerance` must be finite and nonnegative. Zero preserves exact rock
boundaries; larger values simplify collision polygons to reduce vertex counts
and collision cost while the rendered terrain stays detailed. For example, 0.002
allows up to 0.002 scene units of original-vertex deviation from a simplified edge.
Simplification keeps section crossings fixed and rejects invalid loops or changes
that break a block's hole topology. Shapes that cannot safely simplify retain
exact boundaries, so the tolerance does not guarantee a particular vertex count.

After `Update`, obtain collision geometry for a loaded section:

```go
geometry, ready := scene.CollisionGeometry(0)
if ready {
    solid := geometry.Contains(infinicave.V{X: 0.5, Y: -0.4})
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
// Convert local Y to world Y by adding section.Top.
// section.Collision.Polygons already use world coordinates.
```

`GenerateSection` is synchronous and allocates no GPU resources. Run it in a
background goroutine in an interactive application. IDs start at 0 at the bottom
and decrease upward; positive IDs return an error. Each section owns its geometry
and includes one section of padding above and below for seamless generation.
All exposed positions, lengths, radii, and rock heights use scene units; normals
and directions remain unit vectors. The owned local square is `[0, 1] × [0, 1]`,
with generation padding spanning local Y from -1 to 2. `section.WindowTop` marks
the start of that padding in world coordinates. Its owned world band is `[section.Top, section.Top + infinicave.SectionHeight)`;
vegetation may extend outside that band. Collision contours also include padding;
use only the owned band when combining adjacent sections. The exposed grids
describe visual faces, and `section.Collision` contains the collision boundaries.
Use `GenerateSectionWithConfig(config, id)` to share a scene's study, collision
tolerance, and guide loader with synchronous generation. This
package depends on Ebitengine, so desktop initialization still needs a graphical
environment even when only generating geometry.

When migrating from pixel coordinates, divide world positions, lengths, camera
velocities, and collision tolerances by 1000. Generated section-local Y also
subtracts 1 after division, so the owned band starts at zero. Image dimensions
and cursor input from Ebitengine remain pixels.

The API is an initial foundation and may change as gameplay requirements develop.

Background vines use muted colors, a maximum stem width of 0.009 scene units, fine world-aligned grain, and a subtle one-pixel blur to sit behind the raised rock and mushrooms. Grain follows the `-texture` setting; the softened silhouette is baked into each cached vine layer.

Sparse foreground vines grow directly across the raised rock faces, with at most two trunks per section and a few attached branches. These stems use dusty rust and rose colors, subdued highlights, small contact shadows, and seam-following tendrils. Growth stays within the visible rock surface, including shaded faces, and leaves at least 0.03 scene units between the vine ribbons and guide lines to keep the crests clear. Full branches remain continuous across cached section boundaries.

Tiny offshoots attach to the larger vines and trace the actual background cell edges through their junctions. Every other completed offshoot is retained for a lighter density. These slender branches taper over short lengths, inherit their parent’s subdued material, and respect the foreground rock boundaries.

By default, each section grows new random guide curves with varying lengths, directions, and bends. Placement favors underfilled areas while reserving a few open pockets. The finished jagged guides stay at least 0.155 scene units apart, including across section boundaries, and tight folds or self-crossings are rejected. Coverage aims for about two-thirds of the scene within 0.115 scene units of a guide, leaving gaps for platform-game layouts. Seeds reproduce the same scene, but different seeds and sections use fresh shapes rather than fixed curve templates.

Guides form slightly jagged rock borders by cutting the foreground Voronoi cells along broad, uneven facets. Tiny clipping fragments and long, thin foreground cells merge into neighbors on the same side of the guide, favoring compact combined faces. Site spacing compresses across exposed lips and expands into the flanks without adding aligned rows. Flanks use fewer, larger facets; the outer footprint is cut through the cells along a relief contour, rather than ending in a row of whole tiles.

Guides define solid raised formations: a narrow bevel rises to a crest, then a broad flank descends toward the recessed background. The flank steepens toward its foot so the surface turns gradually into shadow. Control-point heights follow this shared relief with small mineral irregularities. Neighboring heights determine one normal per face, and guide-adjacent faces turn toward the guide. A single light from above and left illuminates the rock; a world-aligned depth buffer supplies cast shadows and contact darkening. Existing rock faces stay opaque even in shadow. Exposed boundaries receive shallow side walls and narrow bevels, while internal triangulation remains invisible. A restrained warm-gray palette reserves the lightest tones for crests, with coherent patina on the lower spurs and faint mineral strokes aligned with each facet. Side walls and bevels remain shallow. Background rock retains a darker charcoal material.

Run with `go run ./cmd/infinicave` (Go 1.27 and a graphical desktop). Use `-seed 42` for a reproducible world.

- Mouse wheel / trackpad: scroll vertically; scroll up to explore new terrain.
- Up / Down or W / S: scroll continuously.
- Page Up / Page Down: move by most of a viewport.
- Home / End: return to the starting bottom edge.
- R: generate a new world while keeping the current position.

The window is resizable. Scrolling continues while missing sections are prepared in the background, with “Growing upward…” displayed until they are ready. Rocks share world coordinates across sections, and vines keep their full geometry across boundaries.

Hover over a foreground rock for a soft warm highlight and glow over its entire connected block of cells, including across cached section boundaries. Point within 0.006 scene units of a guide to highlight only that guide line instead. Hover effects are enabled by default; use `-hover=false` to disable them (or `-hover=true` to enable them). PNG exports never include hover effects.

`go run ./cmd/infinicave -seed 42 -output scene.png` exports the bottom 1000 × 2400 pixels and exits. `-texture 0` disables the surface texture. `-collision-tolerance 0.002` simplifies collision polygons with a 0.002-unit tolerance.

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
