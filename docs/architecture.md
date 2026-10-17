# Code navigation

Import `github.com/razzie/ebiten-infinicave` to use the library. The root package
owns the public API and coordinates generation, rendering, queries, and edits.
Implementation packages live under `internal/`, so applications cannot import
them directly.

| Area | Start here | Responsibility |
| --- | --- | --- |
| Configuration and scene lifecycle | `config.go`, `scene.go` | Validate settings; create, update, reset, draw, and close scenes |
| Public geometry | `geometry.go`, `config.go`, `carve.go` | Expose geometry, generation, collision, orientation, and edit entry points with the existing import path |
| Streaming | `world.go`, `world_workers.go`, `world_stream.go`, `uploads.go` | Request sections, coordinate workers and request identities, retain CPU results, budget and publish GPU uploads, and evict distant sections |
| World composition | `scene.go`, `world_render.go`, `carve_render.go`, `carve_burn.go` | Expose custom drawing hooks, draw cached layers and fades, refresh images after edits, and capture removed decorations |
| Queries and identity | `query.go` | Track loaded coverage, formation and guide IDs, aliases, and revisions |
| Geometry algorithms | `internal/geom/` | Vectors, polygon clipping and triangulation, and ray/segment intersections |
| Terrain and vegetation | `internal/terrain/` | Deterministic section generation, guides, Voronoi rock, lighting, vines, mushrooms, collision topology, and geometry edits |
| Meshes and effects | `internal/render/` | Prepare meshes, scale raster coordinates, batch draws, render effects, and manage their GPU resources |
| Viewer | `cmd/infinicave/` | Parse flags, manage the camera and input, draw UI and hover effects, and export PNGs |

Files group complete responsibilities within each package. Add related helpers
and regression tests to the existing owner before creating another file. Distinct
algorithms, resource lifecycles, and build constraints justify separate files;
line counts prompt a readability review rather than an automatic split.

Within `internal/terrain/`, start with these owners:

| Area | Files |
| --- | --- |
| Section generation and loading | `builder.go`, `section.go` |
| Branch strategies and guide geometry | `branching.go`, `guide.go`, `guide_layout.go`, `guide_cells.go` |
| Rock construction and appearance | `rock_grid.go`, `rock_shape.go`, `rock_surface.go` |
| Topology, support identities, collision, and cuts | `topology.go`, `geometry.go`, `collision.go`, `carve.go`, `shape_difference.go` |
| Vine generation | `vines.go`, `path_vines.go`, `vine_fields.go`, `vine_graph.go`, `vine_routes.go`, `vine_growth.go`, `vine_style.go` |
| Supported decorations | `decorations.go`, `mushrooms.go`, `spider_webs.go`, `web_network.go` |
| Numeric kernels | `noise.go`, `raster_scalar.go`, `kernels_default.go`, `kernels_simd.go` |

Within `internal/render/`, `mesh.go` owns mesh contracts and section preparation;
`mesh_batches.go`, `coordinates.go`, `atlas.go`, and `attachments.go` own batching,
raster/material coordinates and image helpers, atlas pages, and attachment
indexing/liveness. Rock faces and boundaries live in `rock.go`, with growth,
grass, vines, supported decorations, and cut masks/rims in `rock_growth.go`,
`grass.go`, `vines.go`, `decorations.go`, and `carve.go`. Effects retain their own
resource lifecycles. In the viewer, `main.go` owns flags, startup, and the Windows
resource generation directive; camera, input features, and the game loop remain
separate.

## Dependencies

```mermaid
graph TD
    Viewer[cmd/infinicave] --> API[infinicave]
    API --> Terrain[internal/terrain]
    API --> Render[internal/render]
    API --> Geometry[internal/geom]
    Render --> Terrain
    Render --> Geometry
    Terrain --> Geometry
```

`geom` and `terrain` have no Ebitengine dependency. Terrain retains the colors,
heights, normals, and topology needed to prepare render meshes. `render` consumes
that data and rendering settings; it never imports the root package or reaches
into a Scene. The background pass receives a narrow drawing interface for the
current cached terrain and foreground silhouette.

Generation and geometry edits stay together because rocks, guides, and plants
share topology and placement rules. Cross-section identity, availability, and
persistent runtime cuts belong to the root, where the world cache is owned.

`Config.RetainSections` controls symmetric retention of loaded sections around
the viewport, while `Config.LookaheadSections` controls extra directional
prefetching beyond the required immediate neighbors. Both counts follow the
scrolling axis in either orientation; retention does not request generation.
When lookahead is enabled, immediate neighbors and directional lookahead also
upload complete GPU layers before becoming visible. Visible layers take priority,
followed by the nearest offscreen sections, within the shared upload budget.

## Ownership and execution

The section worker owns its builder, random streams, guide cache, retained branch paths, and generation timings. It publishes immutable foreground geometry before generating
vegetation or preparing meshes. Foreground-only appearance and a rock-base mesh
then publish without background generation or growth atlas uploads. Background
material retains the compatibility data used by decoration placement. Growth
preparation reuses the base's structural triangles and attachments, fitting only
the later vegetation. It publishes hanging underlay and surface growth images
independently, without replacing the displayed rock base or structural trim.
`Scene.Readiness` exposes the independent layer states. The game goroutine receives that early geometry
and invokes the collision callback. Final vegetation is attached to a separate
geometry value so it cannot mutate already published data.

Mesh preparation uses CPU data, even though vertices use Ebitengine types. GPU
images and shaders are created, uploaded, drawn, resized, and released on the game
goroutine. Upload budgets and cache publication live in `world_stream.go`.
Runnable sections rotate after bounded upload quanta and share draw, index,
pixel-work, and submission-time budgets. Foreground rocks, background rocks,
grass, vegetation, hanging underlay, and surface growth rotate independently
within each section. Background and
grass each retain their own cursor and AA surface, so neither waits for the
foreground image to finish. `uploads.go` owns those publication paths and shared
atlas/material preparation. Starting or publishing an upload does
not require an extra update. The same file owns independent cursors,
images, widths, and reveal clocks for background vines, foreground vines,
mushrooms, and webs. These products can advance alongside terrain uploads and
publish individually as soon as their raster work completes.

`Builder.DecorateProducts` emits immutable per-kind snapshots, including empty
results, without waiting for sibling generators. Each callback prepares and
sends its own mesh. Foreground growth fits from the early appearance alongside
background material/mesh preparation; it does not wait for compatibility
material or decoration generation. The final combined envelope closes the
section job; it is not a publication barrier. Product masks preserve unrelated geometry and meshes, and
recipe identities prevent the final envelope from uploading the same images
again. Newly available products can join an active or parked section upload
without resetting its other cursors. Vegetation readiness waits for all four
plant products at the requested width; `Readiness.Decorations` also includes
growth and grass.

Worker envelopes carry a world epoch and request incarnation. The coordinator
checks both before merging meshes, acknowledging cancellation, or publishing
collision geometry. A late reply cannot finish a newer request for the same
section. Completed request identities remain through retention so unread
collision events can still publish, then expire with eviction; reset creates a
new epoch. Cancellation releases private raster work while retaining published
siblings, their recipes, collision coverage, and reveal clocks. Revisiting an
incomplete section retries its missing products with those images still visible.
Request identities belong to the root coordinator; render recipes remain
independent of request lifetime and raster width. Close drains buffered CPU
replies immediately, and the dispatcher drains again after canceled producers
exit, releasing late replies without blocking the game goroutine on a loader.
Cached refreshes are attempted at most once per section per update; incomplete
recipes cannot request replacement images for missing sibling inputs.

The section phase scheduler and shared helper budget remain. The larger
dependency-graph and progressive-tile changes remain planned in
[render-product-scheduling-design.md](render-product-scheduling-design.md).
`Config.Antialiasing` is fixed for a scene: Full uses geometry antialiasing and
supersampled face uploads; Off and FXAA use native non-antialiased layers and
atlas pages, including carving replay. FXAA owns a reusable final-size target
and filters the scene once after custom passes, grass, bats, and orientation
mapping. CPU geometry and recipes are independent of that raster policy.
Carving replaces connected outer/inner loops independently of material facets.
Carving calls publish geometry, collision, query identities, and results before
returning, and queue texture work with immutable per-cut geometry snapshots.
The next valid update or draw replays that queue in order on the game goroutine,
before uploads replace images or composition uses them. Reset and close discard
queued image edits with the world. Finished foreground rock/vine images receive
cut masks; background rock and background vines remain intact. Removed attachments and overlapping
edits replay only dirty image tiles, independently for each layer. Adjacent dirty
tiles are coalesced into rectangles without filling gaps; surviving growth and
wall recipes are batched in their original texture and blend order for replay.
Vine paths,
leaves and material distances remain immutable; carving never splits their CPU
geometry. Moss and ordinary rock walls use texture erasure without support checks.
Compact commands are selected by section
range and replayed before publication. Resizing reuses immutable bake recipes.
Structural faces retain compact guide support identities through carving and
splits. Destroying the last complete support retires the guide across cached
geometry and queries. Immutable retirement snapshots accompany generation jobs;
builders omit those sources before branch generation and release their resolved
guide cache entries. Late results are filtered again before publication.

Shared CPU atlas samples contain 8×8 grass, moss, and hanging-growth variants,
and paired 16×16 mushroom stem/cap pages. Workers transform and batch the sample
meshes into page coordinates; each world incrementally uploads bounded GPU
pages on the game goroutine. Section recipes retain stable
attachment IDs, inset rock support anchors, visual bounds, and dead flags.
Immutable support indexes contain individual anchors and share detection for
paired grass and tooth records. Filtering returns newly dead records for dirty
tiles and upload liveness, without repeating hit detection or rescaling meshes.
The terrain support query returns mushroom/web IDs directly to image editing;
its index and original objects remain unchanged across deletions. Separate visual
bounds select nearby surviving recipes in their original drawing order.
Finished mushroom groups and web networks are removed only when a support is
cut, without cap, stem or silk intersection checks or regeneration.
Foreground vines use the generated branch graph to guide connected cell-edge
routes; forks attach at facet junctions. Background routes branch twice and use
sparse seam graphs near their seeded paths. Background paths precede
their supporting material. Scene builders allocate no dense vine fields.

`Scene.DrawWithPasses` inserts optional drawing after fog (`BehindRocks`) and
after foreground rocks, mushrooms, webs, and foreground vines (`AboveRocks`).
Foreground grass patches and blades follow that second hook; rear grass blades
share the undergrowth span behind foreground rocks and have no ground patch.
Bats draw last.
`Draw` delegates with empty hooks. Pass callbacks receive a transform from public
world coordinates into the current target, including the intermediate rotated
target used by horizontal scenes. Hooks run once on the game goroutine and are
excluded from offscreen background and shadow composition.

Foreground grass has its own image, with clipped vector ground patches aligned
to the geometric boundary and thick atlas blades rooted along the surface but
angled halfway between each root's surface normal and world-up in compact fans.
Sod uses a shade close to the blade bases. A narrow lip overlaps the boundary
to hide raster fringes, and covers protruding structural teeth with shared
support liveness. Strips cover all eligible silhouette segments independently
of the blade count. Each eligible cell seeds a colony of 2–3 tufts spaced along
connected crest edges. Each tuft follows its local surface slope and independently chooses the
foreground or undergrowth image; rear tufts contain blades alone.
It shares the immutable foreground CPU recipe and supplies the shadow silhouette
alongside rocks, while retaining an independent upload, publication, resize
width, displayed recipe, and reveal clock. A late full foreground recipe can
supply grass while the early rock base is still uploading. Rock publication
preserves already-published grass. Grass edits replay from its own displayed
recipe and width; removing a staged grass attachment restarts only that private
grass upload. `Readiness.Decorations` includes visible grass. Mushrooms and webs use cropped
images published independently from other vegetation. During resize each image
keeps its own displayed width until its replacement finishes; aggregate readiness
waits for all required replacements. Displayed rock and plant recipes retain their
own physical snapshots separately from newer CPU recipes. Carving replays each displayed image from the recipe
that supplied its pixels, including old-width plants during replacement uploads.
Early decoration publication cannot change rock replay or growth transitions.
Carving updates already-published products
even while sibling images or supporting rock images are still uploading.
The viewer's optional `ribbon.go`
trail demonstrates both hooks with mouse input and world coordinates,
advancing its samples during `Game.Update`.

Optional decoration burn dissolves capture cropped removed growth recipes and
foreground vine pixels during runtime carving. `internal/render` owns their
shader, animation, and temporary images; the root coordinates capture and draws
remnants on the original side of `AboveRocks`. Geometry changes immediately;
cached images and burn captures follow when queued texture edits run before the
next update or draw. Snapshots expire during `Update` and are released on reset
or close; stored cuts, loading, and upload replay never start animations.

Material shaders reconstruct canonical artwork coordinates from raster sample
centers. Scaling, cropping, and supersampling preserve that frame, including
carved rims, so replay keeps grain aligned at crop boundaries.

Foreground composition preserves hanging growth → structural underlay and
faces/outlines → surface growth → structural top trim and cut rims. Structural
trim publishes with rocks; growth products keep their own images, recipes,
widths, and reveal clocks. Each product accepts edits at its displayed width,
and overlapping cuts replay only its affected tiles. Support removal restarts
only the affected private growth upload. Burns capture the product's published
pixels and reveal progress. Foreground grass remains above `AboveRocks`.

Optional appearance fades blend in each cached rock or vegetation layer as it
publishes. The root advances independent clocks in `Update`; ordinary drawing
and shadow composition share them. Growth never replaces rocks or restarts their
reveal. During a structural reveal, one reusable scratch image composes the
ordered foreground span before applying structural opacity, preserving uniform
fading where structural layers overlap. Fully revealed rocks draw the cached
spans directly. Crossing plants respect their supporting band's reveal. Resize
retains old-width products until replacements publish and does not restart
reveals. Eviction, reset, and close release the span images and scratch image.
Readiness describes published resources independently of animation progress.

Synchronous generation uses `terrain.GenerationOptions`, populated by the public
`GenerateSectionWithConfig` entry point after validating the full configuration.
Its collision callback runs on the calling goroutine.

## Tests and public compatibility

Tests live in the package that owns the behavior and use the corresponding
source filename (`coordinates.go` / `coordinates_test.go`, for example).
Tests for helpers that share a consolidated owner stay together in its companion
test file.

Scene configuration and effect lifecycle tests belong in root `scene_test.go`,
including fog, background, and bat enablement, updates, reset, and close.
Independent effect and mesh behavior belongs in `internal/render/`. Streaming
and GPU upload integration tests belong in root `world_test.go`,
`world_stream_test.go`, `world_workers_test.go`, and `uploads_test.go`; public
section generation and collision access belong in root `geometry_test.go`.
Terrain generation, caching, and geometry tests live beside their implementations
in `internal/terrain/`.

External API tests in `api_test.go` exercise the public import path. Shared fixture
helpers stay within their test package and use an area prefix so editor tabs are
easy to distinguish: root `scene_fixtures_test.go`,
`internal/render/render_fixtures_test.go`, and
`internal/terrain/terrain_fixtures_test.go`. Benchmarks use the owner's test file
or an owner-prefixed `*_bench_test.go` file. These API, fixture, and benchmark
files, plus the separate opt-in `visual_capture_test.go` and
`carve_latency_test.go` diagnostic harnesses, are the exceptions to matching source
filenames. Preserve subprocess test names and environment-variable controls when
moving tests.

Public geometry types are aliases to their implementation types. Exported names,
fields, methods, constants, and function signatures are preserved. Aliases do
change the defining package reported by reflection for moved types; code relying
on `reflect.Type.PkgPath()` or `%T` should account for that. Internal geometry
helpers are package functions so they do not become methods on aliased public
types.

Run `go test ./...` and `go vet ./...`. CPU geometry tests can also run separately
with `go test ./internal/geom ./internal/terrain`. Rendering and scene checks may
need a graphical environment. Fixed-seed exports in both orientations and the
existing generation/carving benchmarks provide behavior and cost comparisons.
