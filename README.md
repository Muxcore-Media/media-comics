# Media Comics

Manga / comic **series + issue** library manager for MuxCore.

Exposes `muxcore.comics.v1.ComicManagementService` (series/issues CRUD, scan, missing list, import) with **SQLite persistence** under the data directory, plus SettingsProvider for `library_dir` / `data_dir`.

Library scanning walks the configured root for local comic stubs (`.cbz`, `.cbr`, `.pdf`, `.epub`, …) and upserts series/issues from the path layout `Series/issue.ext`. Metadata is derived from filenames only — **no paid or network metadata APIs**.

## Ports

| Service | Default |
|---------|---------|
| gRPC | `:9660` |
| HTTP | `:9661` (`/healthz` + JSON API below) |

## HTTP API

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/series` | List series (`?q=` filter) |
| GET | `/api/series/{id}` | Series detail + issues |
| GET | `/api/issues` | List issues (`?series_id=`) |
| GET | `/api/missing` | Monitored issues with no on-disk file (`?page=`, `?page_size=`) |
| POST | `/api/scan` | Rescan `library_dir` into SQLite |
| POST | `/api/issues/{id}/import` | Attach an existing file under `library_dir` (`{"path":"..."}`) |
| GET | `/api/issues/{id}/stream` | Stream the issue archive (path must stay under `library_dir`) |

Startup runs an initial library scan after the store opens.

## Data layout

| Path | Purpose |
|------|---------|
| `$COMICS_DATA_DIR/comics.db` (default `./data/comics.db`) | SQLite library |
| `$COMICS_LIBRARY_DIR` (default `$COMICS_DATA_DIR`) | Comic files to scan |

Set `COMICS_LIBRARY_DIR` when comic archives live outside the data directory (e.g. vault `COMICS_DATA_DIR=$DATA/comics` with files directly in that folder).

## Build / test

```bash
export PATH=$HOME/.local/go/bin:$PATH
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go build -o bin/media-comics ./cmd/module
```

Offline tests use `internal/testdata/library` (empty `.cbz` / `.pdf` stubs).

## Status

v0.2.0 — SQLite persistence, library scan/import/stream, missing-issue tracking, and gRPC parity for disk state (`path`, `has_file`, `ListMissing`, `ImportIssue`).
