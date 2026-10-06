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
Generation, spatial fields, and collision calculations use scene units throughout.
Only mesh preparation and rendering convert coordinates to pixels. Set
`scene.SetRenderWidth(nativeWidth)` before `Update` to rasterize cached textures
at the native screen width; the default is 1000 pixels per scene unit. Resizing
reuses prepared meshes, queries, and runtime cuts. Sections contributing to the
viewport refresh first; offscreen images refresh when they become visible.

The viewer implements hover locally using `Query`, `Formation`, and `Guide`;
applications can use the same APIs to draw their own selection effects.
`scene.Reset(seed)` discards cached sections and starts a new
world with the same rendering settings, section content loader, and collision callback. Rendering does not include UI or exports.
Call every `Scene` method on the Ebitengine game goroutine. `Close` is idempotent
and releases shaders and cached images; an in-progress CPU generation finishes
before its worker exits.

`DefaultConfig` uses texture strength 8, moving fog, animated bats, shaded rendering,
seed 0, and exact collision geometry. A zero `Config` is also valid and disables
texture, fog, and bats. `View`
is a typed enum:

```go
config.Fog = false // disable the moving mist
config.Bats = false // disable the occasional flying bats
config.View = infinicave.ViewShaded // or ViewClay, ViewHeight, ViewNormals, ViewShadows
config.CollisionTolerance = 0.002 // approximation tolerance in scene units
```

`Texture` accepts 0–16. Diagnostic views disable texture, fog, and bats and hide vegetation.
Fog drifts over the background rock and vines, behind mushrooms and foreground
rock and vines. It follows world coordinates across section seams, scrolling,
and resizing, and advances once per `Scene.Update` so repeated draws share the
same animation state. Set `Config.Fog` to toggle it when creating a scene.
`ParseView` converts strings such as `"normals"` for
command-line tools; library code can use the constants directly.

`Config.Bats` enables small bats that occasionally enter either side of the
viewport and fly across on curved paths, with swoops, varying speed, and banking
as their wings flap. `Scene.Update` advances their animation once per tick;
repeated `Scene.Draw` calls share the same animation state. Their paths stay in
world coordinates as you scroll or resize. `Scene.Reset` clears existing bats
and starts a fresh, seed-reproducible arrival sequence.

Set `Config.LoadSection` to supply authored guides and holes per section:

```go
config.LoadSection = func(id int64) infinicave.SectionContent {
    if id != 0 {
        return infinicave.SectionContent{}
    }
    return infinicave.SectionContent{
        Guides: []infinicave.Guide{{
            Pts: []infinicave.V{{X: 0.2, Y: 0.4}, {X: 0.7, Y: 0.5}},
        }},
        Holes: []infinicave.Hole{
            {Shape: infinicave.HoleCircle,
                Center: infinicave.V{X: 0.4, Y: 0.47}, Radius: 0.04},
            {Shape: infinicave.HoleSegment,
                Start: infinicave.V{X: 0.5, Y: 0.3},
                End: infinicave.V{X: 0.6, Y: 0.6}, Width: 0.02},
        },
    }
}
```

`SectionLoader` now returns `SectionContent{Guides, Holes}`, replacing the old
`GuideLoader` / `Config.LoadGuides` callback. The content structure can grow to
include more authored objects. Assign the callback before `NewScene`, or use it
with `GenerateSectionWithConfig`.

All points use section-local scene units in the owned unit square. Generation
copies the callback's slices. Guide arc lengths and bounds are recomputed;
only `Pts` is required. `BrightSign` defaults to 1; use -1 to reverse the lit
side. Zero guide seeds receive stable seeds derived from the world seed,
section ID, and guide index. Supplied guides keep their shapes and placement;
custom layouts are responsible for spacing.

Holes carve the shaped foreground and damage its generated vegetation. Circles use `Center`
and `Radius`; segments use `Start`, `End`, and full `Width`, with flat ends.
Authored holes can cross seams, but their bounds must stay within local Y = -1
to 2; describe longer cuts using several section-local holes. Invalid holes and
invalid guides (nonfinite points or fewer than two distinct consecutive points)
are ignored. `Section.Holes` includes the valid holes in its padded window.

A nil `LoadSection` uses random generation. A callback
returning empty content produces no guides or foreground rock for that section.
Neighboring sections are also loaded
for seamless padding; IDs may be requested repeatedly and in any order. Return
consistent content per ID. Scene calls the loader on its background worker;
synchronous generation calls it directly. Protect shared mutable state against
concurrent calls, including during `Reset`.

Foreground geometry is always prepared and retained, including in diagnostic
views. Client selection effects have no effect on collision availability.

Set `Config.OnCollisionReady` before creating a scene to set up physics as soon
as a section's foreground polygons are available:

```go
config.OnCollisionReady = func(geometry infinicave.CollisionGeometry) {
    // Create or replace your game's collision shapes for geometry.ID.
    // geometry.Polygons are union boundaries in world-space scene units.
}
```

`Scene.Update` invokes this optional callback on the game goroutine, before
vegetation generation, mesh preparation, or GPU uploads have to finish. It also
notifies for prefetched sections and empty geometry. Polygons include authored
holes, stored runtime cuts, and `CollisionTolerance`; the callback owns its copy.
`scene.CollisionGeometry(id)` is available at this point too. Eviction and
regeneration notify again; resizing does not. `Reset` keeps the callback, so
clear your game's old collision shapes when resetting. Runtime carving uses
`CarveResult.SectionIDs` to identify collision shapes to refresh.
`GenerateSectionWithConfig` invokes the callback synchronously on its caller's
goroutine, before generating vegetation.

World queries run independently of the camera and rendering. After `Update`,
cast a finite ray in world coordinates and scene units:

```go
result, err := scene.Query(infinicave.Ray{
    Origin:      infinicave.V{X: 0.1, Y: -0.4},
    Direction:   infinicave.V{X: 1, Y: 0},
    MaxDistance: 0.8,
}, infinicave.QueryOptions{
    Targets:     infinicave.TargetRock | infinicave.TargetGuide,
    GuideRadius: 0.006,
})
if err != nil {
    return err
}
if !result.Complete {
    // Terrain before a possible hit is not loaded; retry after loading it.
} else if result.Found {
    hit := result.Hit // Point, Normal, Distance, StartedInside, and object ID
    switch hit.Kind {
    case infinicave.TargetRock:
        formation, available := scene.Formation(hit.FormationID)
        _ = formation // union boundary Polygons, Min/Max, SectionIDs, Complete
        _ = available
    case infinicave.TargetGuide:
        guide, available := scene.Guide(hit.GuideID)
        _ = guide // world-space Points, cumulative arc lengths S, Min/Max
        _ = available
    }
}
```

A zero `Direction` or zero `MaxDistance` performs a point query. Directions are
normalized internally, so their magnitude does not change the cast length.
Zero `Targets` selects rocks and guides. A zero guide radius intersects the
polyline itself; a positive radius includes round end caps. The nearest target
wins, with guides winning equal-distance ties. Rock queries use exact union
boundaries, accounting for holes and removing internal cell and section edges;
`CollisionTolerance` does not change query geometry. Starting in rock or within
a guide's radius returns distance zero, `StartedInside=true`, and a zero normal.
Point queries also return zero normals.

`Complete` distinguishes a confirmed miss from unavailable terrain. Queries
never generate sections and cannot confirm a hit beyond an unloaded gap. A hit
before a gap is complete even if the rest of the requested ray is not loaded.
The world occupies X from 0 to 1 and Y at or above the floor (Y <= 0); portions
of a ray outside that strip are known empty. Guide-radius queries conservatively
require loaded neighboring bands within the radius in Y.

Formation and guide IDs are opaque, comparable references scoped to one world.
They survive partial cache eviction and neighboring-section reloads while the
object remains cached. Newly discovered formation connections keep earlier IDs
fetchable as aliases; `Formation.ID` gives the current canonical ID. Full object
eviction, `Reset`, and `Close` invalidate references. IDs are runtime references,
not persistent save-file keys. Fetches return independent copies. Formation
geometry contains only its currently loaded owned section bands, without
duplicated padding; `Formation.Complete` reports whether it continues into
unloaded terrain. Incomplete outlines have closing edges at the cache limits.
Fetched guide polylines are complete. No individual face or shading details are
exposed by these APIs, and their coordinates and IDs do not depend on pixel
resolution.

Use `scene.GeometryRevision()` to detect changes before reusing fetched geometry.
Formation IDs can stay valid while their cached outlines grow, shrink, or merge.
The scene-wide revision advances when early collision geometry arrives, terrain
becomes queryable, sections are evicted, or terrain is edited. Query publication
advances it separately from early collision availability. It also advances on
`Reset` and the first `Close`, stays monotonic across resets, and is unaffected by
resizing, rendering, or reads. Compare versions within one scene; a difference
means cached geometry may be stale, not a count of changes. Refetch affected IDs
and handle unavailable objects; formation fetches resolve merge aliases.

```go
// After scene.Update and any runtime edits:
revision := scene.GeometryRevision()
if revision != cachedRevision {
    // Invalidate your cached geometry and refetch objects as needed.
    cachedRevision = revision
}
```

Carve foreground rock at runtime with world coordinates:

```go
blast, err := scene.CarveCircle(infinicave.V{X: 0.4, Y: -0.53}, 0.06)
drill, err := scene.CarveSegment(
    infinicave.V{X: 0.5, Y: -0.7},
    infinicave.V{X: 0.6, Y: -0.4}, 0.02,
)
// scene.Carve(Hole{...}) accepts the same descriptors as SectionContent,
// with world coordinates rather than section-local coordinates.
_ = blast
_ = drill
_ = err
```

`CarveResult.Changes` maps each affected `Before` formation ID to its `Remaining`
IDs: zero means destroyed, one means reshaped, and two or more means split.
Affected IDs and their aliases expire; fetch the new parts with `scene.Formation`.
Unaffected rock and guide IDs remain valid. `SectionIDs` lists edited collision
sections; refetch their geometry if your physics engine caches it. Both the
result and each change have `Complete` flags. When a cut or formation extends
into unloaded terrain, the split report is provisional; the cut still applies
when that terrain loads. Cuts survive eviction and reload and are cleared by
`Reset`. Previously returned geometry copies remain independent snapshots.

Circle cuts use a 96-sided inscribed polygon, with maximum radial deviation of
about 0.054% of the radius; segment cuts are rectangles with flat ends. Inputs
must be finite and sizes positive. Cut boundaries drive collision, exact queries,
hover, and rendering together. Shaded and clay views add dark recessed walls and
uneven warm mineral rims to suggest chipped rock and depth. Background rock
remains in place. Mushrooms are removed individually when
their buried roots lose contact with rock. Both vine layers are cut through the
hole, retaining the pieces on either side and remapping surviving branches.
Vine shading stays with its original family; ribbons and contact shadows are
clipped to the opening after blur. Authored holes and runtime cuts use the same
vegetation damage, so reloads cannot restore removed mushrooms or regrow vines.
Carving runs synchronously on the game goroutine, including rebuilding affected
foreground and vegetation images; large cuts can take
longer than a normal frame.

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
Use `GenerateSectionWithConfig(config, id)` to share a scene's seed, collision
tolerance, and section content loader with synchronous generation. This
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
- Left mouse: press and hold to grow a blast; move the cursor to select a drill
  rectangle from the initial world point to the current cursor. Release to carve.
- Right mouse: cancel the current action, including while left remains held.
- R: generate a new world while keeping the current position; clears runtime cuts.

A warm outline previews the cut while held; terrain changes only on release.
The drill is 0.02 scene units wide. The status text reports affected, split, and
destroyed formations, marking partial reports when terrain is unloaded.

The viewer enables the library's bats by default; use `-bats=false` to disable
them. The viewer disables bats for PNG exports.

The viewer renders at the window's native pixel resolution, including on HiDPI displays. Resizing rerasterizes visible sections using cached geometry and preserves runtime cuts and camera position. Offscreen sections refresh when they become visible. Scrolling continues while missing sections are prepared in the background, with “Growing upward…” displayed until they are ready. Rocks share world coordinates across sections, and vines keep their full geometry across boundaries.

Hover over a foreground rock for a soft warm highlight and glow over its entire connected block of cells, including across cached section boundaries. Point within 0.006 scene units of a guide to highlight only that guide line instead. Hover effects are disabled by default; use `-hover=true` to enable them. PNG exports never include hover effects.

`go run ./cmd/infinicave -seed 42 -output scene.png` exports the bottom 1000 × 2400 pixels and exits. `-texture 0` disables the surface texture. `-fog=false` disables the moving fog. `-collision-tolerance 0.002` simplifies collision polygons with a 0.002-unit tolerance.

The `-view` options are `shaded` (default), `clay`, `height`, `normals`, and `shadows`. Diagnostic views disable texture, fog, and vegetation; clay uses neutral gray material with the same lighting and exposed edges.

```sh
go run ./cmd/infinicave -seed 42 -view clay -output clay.png
go run ./cmd/infinicave -seed 42 -view height -output height.png
go run ./cmd/infinicave -seed 42 -view normals -output normals.png
go run ./cmd/infinicave -seed 42 -view shadows -output shadows.png
```

Relief, shadows, rock triangulation, outlines, and vine and mushroom meshes are prepared in the background once per cached section. Background rock, foreground rock, both vine layers, and mushrooms are polygonized concurrently, using up to `GOMAXPROCS - 1` workers (at least one) to leave CPU capacity for rendering. The game loop uploads meshes with limits on submission time, triangle indices, and draw calls per tick. Collision polygons are published before vegetation generation and mesh preparation; complete terrain uploads first, and vegetation appears when all its layers finish. Vegetation meshes are batched in draw order, and their textures are cropped to occupied bounds while retaining world-aligned grain. Vine steering uses reusable fields and linear distance sweeps; guide proposals are cached in a bounded window and rock adjacency uses spatial buckets. These caches do not affect geometry: the same seed, section, and guide inputs reproduce the same result regardless of load order or eviction. The renderer uses a shallow 2.5D surface: illumination is constant within each polygon, with depth shadows averaged over the face to preserve the faceted appearance.

Run checks with `go test ./...` and `go vet ./...`. Ebitengine initializes the display for tests, so these also need a graphical session.
