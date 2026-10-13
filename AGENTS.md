# Agent Guide

## Project Shape

- The root `infinicave` package owns the public API and coordinates scenes, streaming, rendering, queries, collision, and edits.
- `internal/geom` contains shared geometric primitives; `internal/terrain` owns deterministic terrain generation and topology; `internal/render` prepares and draws terrain; `cmd/infinicave` is the interactive viewer and PNG exporter.
- Start with [docs/architecture.md](docs/architecture.md) for package ownership and lifecycle details. Keep changes within the package that owns the behavior and preserve the root package's public import path.

## Working Agreements

- Follow nearby code and test conventions. Keep tests beside their owning implementation; public import-path coverage belongs in `api_test.go`.
- Preserve deterministic output for a fixed seed and inputs. When changing generated geometry or render behavior, consider section boundaries, reload/eviction, both orientations, carving, and resizing.
- Treat exported names, fields, methods, constants, and signatures as public compatibility surface. Update public documentation when behavior or configuration changes.
- Scene methods, Ebitengine images/shaders, uploads, and drawing belong on the Ebitengine game goroutine. Background generation must publish immutable results for the game goroutine.
- Avoid unrelated refactors. Keep implementation details in their owning packages and user-facing API guidance in `docs/usage.md`.

## Verification

Run focused package tests while iterating, then the repository checks:

```sh
go test ./...
go vet ./...
```

Some rendering and scene tests initialize Ebitengine and require a graphical session. CPU-only geometry tests can be run with:

```sh
go test ./internal/geom ./internal/terrain
```