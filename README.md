# Media Comics

Manga / comic **series + issue** library manager for MuxCore.

Exposes `muxcore.comics.v1.ComicManagementService` (series/issues CRUD) with **SQLite persistence** under the data directory, plus SettingsProvider for `library_dir` / `data_dir`.

Library scanning walks the configured root for local comic stubs (`.cbz`, `.cbr`, `.pdf`, `.epub`, …) and upserts series/issues from the path layout `Series/issue.ext`. Metadata is derived from filenames only — **no paid or network metadata APIs**.

## Ports

| Service | Default |
|---------|---------|
| gRPC | `:9660` |
| Health | `:9661` (`/healthz`, JSON stubs `GET /api/series`, `GET /api/series/{id}`, `GET /api/issues`) |

## Data layout

| Path | Purpose |
|------|---------|
| `$COMICS_DATA_DIR/comics.db` (default `./data/comics.db`) | SQLite library |
| `$COMICS_LIBRARY_DIR` (default `./data/comics`) | Comic files to scan |

## Build / test

```bash
export PATH=$HOME/.local/go/bin:$PATH
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go build -o bin/media-comics ./cmd/module
```

Offline tests use `internal/testdata/library` (empty `.cbz` / `.pdf` stubs).

## Status

v0.2.0 — SQLite persistence + library root scan fixtures. ComicVine/AniList search and acquisition wiring remain follow-ups.
