# Library Usage

This guide covers the public workflows for using the library. For the package map and implementation lifecycle, see [Architecture](architecture.md).

## Create and Update a Scene

Create one `Scene`, retain it for your game, and close it when the game exits. Call scene methods from the Ebitengine game goroutine.

```go
config := infinicave.DefaultConfig()
config.Seed = 42
scene, err := infinicave.NewScene(config)
if err != nil {
    return err
}
defer scene.Close()

// In Update, once per tick:
viewport := infinicave.Viewport{Y: -1, Height: 1}
ready := scene.Update(viewport)

// In Draw:
scene.Draw(screen, viewport)
```

`Update` requests terrain for the viewport and reports whether it is ready. It also advances animated effects. Repeated draws do not advance animation. The library does not own your game loop, camera, input, window settings, or UI. `Close` is idempotent and releases scene rendering resources. `Reset(seed)` starts a new world while retaining the scene's settings and callbacks.

`scene.Readiness(viewport)` reports `Collision`, `Foreground`, `Background`, and
`Decorations` independently without requesting generation. Foreground rock faces,
walls, and edge detail publish before background generation and growth atlas
uploads. Growth and grass arrive later through a replacement foreground image;
the published rock base stays drawable while that image uploads. Continue to
call `Draw` during loading. Use `Update`'s full-readiness result for exports.

## Coordinates and Orientation

All positions, distances, radii, velocities, and collision tolerances use scene units. A section is one unit square and `infinicave.Width` and `infinicave.SectionHeight` are each 1.

Vertical is the default orientation. The cave occupies X from 0 to 1 and extends upward from the floor at Y = 0 into negative Y. Section IDs are 0, -1, -2, ... upward. A vertical viewport uses `Y` and `Height`; for example, `{Y: -1, Height: 1}` shows the bottom section.

Set `Config.Orientation = infinicave.Horizontal` before `NewScene` to grow rightward. The cave then occupies Y from 0 to 1 and X >= 0. Section IDs decrease to the right. Horizontal viewports use `X` and `Width`, such as `{X: 0, Width: 1.6}`. Orientation is fixed for the lifetime of a scene, including across resets.

Viewport dimensions must be finite and positive. Vertical `Y` must be finite and no greater than `-Height`; horizontal `X` must be finite and nonnegative. `Viewport.Velocity` is movement per tick along the scrolling axis and is used for directional prefetching.

Rendering uses the smaller target dimension as pixels per scene unit. Portrait and landscape targets center the cave on the bounded axis. Set `scene.SetRenderWidth(min(nativeWidth, nativeHeight))` before `Update` to match cached raster resolution to the target; the default is 1000 pixels per scene unit. Resizing reuses prepared geometry and preserves runtime cuts. Visible uploads take priority; when lookahead is enabled, neighbors and directional lookahead sections refresh afterward before entering the viewport.

## Rendering Hooks

Use `DrawWithPasses` to draw custom content behind or above the foreground rocks:

```go
scene.DrawWithPasses(screen, viewport, infinicave.RenderPasses{
    BehindRocks: func(ctx infinicave.RenderContext) {
        // Draw after background effects and fog, before foreground rocks.
    },
    AboveRocks: func(ctx infinicave.RenderContext) {
        // Draw after foreground rocks, mushrooms, webs, and foreground vines.
    },
})
```

Callbacks run once per valid draw, including while terrain is loading. They should draw only; do not update or mutate the scene from a callback. `ctx.WorldToTarget` maps world coordinates to the target. `ctx.Min` and `ctx.Max` bound the target in world units. In horizontal scenes, `ctx.Dst` is a temporary rotated target and should only be borrowed for the callback. `Draw` is equivalent to `DrawWithPasses` with no callbacks.

The order is background and fog, `BehindRocks`, foreground rocks, mushrooms and webs, foreground vines, `AboveRocks`, grass, then bats. Custom content is not included in background blur or shadow masks.

## Configuration

`DefaultConfig()` selects the viewer's appearance. A zero `Config` is valid and disables optional texture, effects, bats, and vegetation. Common settings include:

| Setting | Purpose |
| --- | --- |
| `Seed` | Reproducible world generation. |
| `Orientation` | `Vertical` or `Horizontal`; fixed for the scene's lifetime. |
| `RetainSections` | Keep published sections this far beyond each end of the viewport along the scrolling axis; default 3, nonnegative. |
| `Antialiasing` | `AntialiasingFull` (default and zero value), `AntialiasingOff`, or `AntialiasingFXAA`. |
| `LookaheadSections` | Prefetch this many extra sections beyond the immediate neighbor in the direction of travel; default 4, nonnegative. Zero disables lookahead. |
| `BranchingStrategy` | `BranchingSimple`, `BranchingRecursive`, or `BranchingLoops`. |
| `Texture` | Surface grain strength, from 0 to 16. |
| `Fog`, `BatsPerMinute` | Animated atmosphere and bat arrivals; zero disables each. |
| `MushroomDensity`, `MossDensity`, `GrassDensity`, `HangingGrowthDensity`, `SpiderWebDensity` | Decoration occurrence or coverage from 0 to 1. |
| `BackgroundBlur`, `ShadowBlur` | Softness in scene units, from 0 to 0.05. |
| `DisableBackground` | Skip background rocks, vines, blur, shadows, and their GPU uploads. Defaults to false; fog remains controlled by `Fog`. |
| `ShadowOpacity` | Dynamic background shadow opacity from 0 to 1. |
| `CollisionTolerance` | Optional collision polygon simplification tolerance; zero keeps exact boundaries. |
| `View` | `ViewShaded`, `ViewClay`, `ViewHeight`, `ViewNormals`, or `ViewShadows`. |

Diagnostic views hide vegetation and disable texture, background effects, fog, and bats. See the `Config` documentation for validation ranges and complete defaults.

Full antialiases geometry and atlas baking. Off disables geometry antialiasing
and supersampling throughout uploads, bats, and carving replay. FXAA uses the
same non-antialiased geometry and filters the complete scene once at native
output resolution, after both render hooks, grass, bats, and horizontal rotation.
It preserves transparent margins and works while layers are still loading.
Background blur and shadow softness remain controlled by their own settings.
Custom hooks can use `context.Antialiasing.GeometryAA()` for triangle/path draw
options; the library cannot override options chosen inside callbacks. Content
drawn into the destination before or after `Scene.Draw` is outside the FXAA pass.
Choose a mode before `NewScene`; it survives Reset and resizing. The viewer flag
is `-antialiasing full|off|fxaa` and also applies to PNG exports.

Streaming uses the same two counts in both orientations: retention applies to up/down in vertical scenes and left/right in horizontal scenes, while lookahead follows `Viewport.Velocity`. Retention preserves published CPU geometry, meshes, and uploaded images without requesting new sections. Lookahead requests and retains extra sections only while moving; its count is independent of speed. Requests and unpublished results share a bounded queue, so long lookahead windows fill as capacity permits. Required viewport sections take priority over unpublished speculative results. Unpublished results and paused uploads outside the request window can be discarded to make room for the current viewport. Both settings survive `Reset`.

Immediate neighbors remain requested and retained because their vegetation can reach into the viewport. `Update` waits for missing required sections and uploads that contribute to the viewport; purely offscreen uploads and extra lookahead do not delay readiness. Lookahead and an active GPU upload may extend beyond the retention range. A zero `Config` keeps only visible sections and required neighbors, with no extra retention or lookahead. Defaults now use symmetric retention and fixed lookahead, replacing the previous asymmetric retention and speed-dependent lookahead.

With `LookaheadSections > 0`, prefetching includes GPU uploads for rocks, vines, and decorations, so completed sections are ready to draw before they enter the viewport. Immediate neighbors also prepare all layers, including while stationary. Visible uploads take priority, then the nearest offscreen sections finish before farther ones. Offscreen uploads do not delay viewport readiness; generation and uploads still advance within their per-tick budgets. Zero lookahead keeps offscreen rock images deferred and uploads neighboring plants only when they contribute to the viewport.

Set `config.DisableBackground = true` to draw the foreground over your own background. This leaves generation and collision/query geometry unchanged. Set `config.Fog = 0` too if you want to disable mist.

## Authored Sections

Set `Config.LoadSection` before creating the scene to supply guides and holes. The callback receives section IDs, which may be requested repeatedly and in any order; return consistent content for each ID.

```go
config.LoadSection = func(id int64) infinicave.SectionContent {
    if id != 0 {
        return infinicave.SectionContent{}
    }
    return infinicave.SectionContent{
        Guides: []infinicave.Guide{{
            Pts: []infinicave.V{{X: 0.2, Y: 0.4}, {X: 0.7, Y: 0.5}},
        }},
        Holes: []infinicave.Hole{{
            Shape: infinicave.HoleCircle,
            Center: infinicave.V{X: 0.4, Y: 0.47}, Radius: 0.04,
        }},
    }
}
```

Points use section-local coordinates in the owned unit square. Generation copies callback slices and recomputes guide lengths and bounds. Invalid guides and holes are ignored. Holes may cross section boundaries, but their bounds must stay within one section of padding along the scrolling axis; split longer authored cuts across sections. A nil loader uses procedural guides, while empty content produces no foreground rock for that section. Neighbor sections are loaded for seamless padding. Scene loader calls run on a background worker and must be safe for concurrent calls across reset; synchronous generation calls the loader directly.

## Queries and Collision

Queries use world coordinates and do not generate terrain. A ray query can target rock, guides, or both:

```go
result, err := scene.Query(infinicave.Ray{
    Origin: infinicave.V{X: 0.1, Y: -0.4},
    Direction: infinicave.V{X: 1, Y: 0},
    MaxDistance: 0.8,
}, infinicave.QueryOptions{
    Targets: infinicave.TargetRock | infinicave.TargetGuide,
    GuideRadius: 0.006,
})
```

Check `result.Complete` before treating a miss as final: terrain along the ray may not be loaded. `result.Found` indicates a hit, with point, normal, distance, target kind, and object ID. A zero direction or maximum distance performs a point query. Rock hits use exact union boundaries, including authored holes and runtime cuts.

Use `scene.CollisionGeometry(id)` for a loaded section's world-space rock polygons. The returned geometry is an independent copy. `Config.OnCollisionReady` can notify your game as soon as collision polygons are available, before render layers finish; the callback runs on the game goroutine during `Update`. Runtime carving reports affected sections through `CarveResult.SectionIDs`, which can be used to refresh physics shapes.

`Formation` and `Guide` APIs return geometry for query hits. IDs are runtime references, not persistent save keys; they can become unavailable after full eviction, reset, or close. Use `scene.GeometryRevision()` to detect when cached fetched geometry may need refreshing.

## Carving

Carve with world-space circle or segment cuts:

```go
result, err := scene.CarveCircle(infinicave.V{X: 0.4, Y: -0.53}, 0.06)
if err != nil {
    return err
}
_ = result
```

`CarveSegment(start, end, width)` makes a flat-ended cut; `Carve(Hole)` accepts the same descriptors as authored holes, using world coordinates. Inputs must be finite and sizes positive. Cuts update rendering, collision, and exact queries together, persist through section eviction, and are cleared by `Reset`.

Foreground vines, their leaves, moss and rock surface details are carved by erasing their rendered textures. Background rock and background vines remain intact. Original geometry remains available for baking after resizing or reloading; vine paths returned by section generation are not split around holes. Mushroom groups disappear when any inset root support is cut, and webs disappear when any rock attachment support is cut. Cutting only a mushroom cap, stalk or free web strand retains the supported decoration. Grass and hanging growth are erased locally, with the whole tuft or instance removed when a support anchor is cut.

`CarveResult.Changes` maps affected formation IDs to the remaining formations: no IDs means destroyed, one means reshaped, and multiple means split. Reports can be provisional when a cut reaches unloaded terrain. `SectionIDs` identifies collision sections to refresh. Previously returned geometry remains an independent snapshot.

When a formation is completely destroyed, its guides are released and their guide IDs expire once no other supported fragment or unloaded continuation remains. Compact source identities persist with the edits, so reloaded sections skip those guides and their procedural branches. `Reset` clears these exclusions too.

## Generate a Section Without a Scene

`GenerateSection(seed, id)` synchronously generates section data without creating a renderer or GPU resources:

```go
section, err := infinicave.GenerateSection(42, 0)
if err != nil {
    return err
}
_ = section.Foreground
```

It defaults to vertical orientation; use `GenerateSectionWithConfig(config, id)` to share scene settings, horizontal orientation, collision tolerance, and the section loader. Generation may be expensive, so call it from a background goroutine in an interactive application. Ebitengine still requires a graphical environment for application initialization.

## Viewer and Export

Run `go run ./cmd/infinicave -h` for the complete flag list. Useful examples:

```sh
go run ./cmd/infinicave -seed 42 -mode landscape
go run ./cmd/infinicave -seed 42 -branching loops
go run ./cmd/infinicave -seed 42 -disable-background
go run ./cmd/infinicave -retain-sections 2 -lookahead-sections 1
go run ./cmd/infinicave -seed 42 -view normals -output normals.png
```

The viewer defaults to portrait/vertical scrolling. Arrow keys or W/S scroll vertically; in landscape mode use Left/Right or A/D. Page Up and Page Down move by most of a viewport; Home and End return to the starting edge. Hold the left mouse button to carve, and press R to regenerate while keeping the current position. Press Esc to exit the viewer. PNG export captures the first 2400 pixels along the scrolling axis and exits; bats and hover effects are disabled for exports.
