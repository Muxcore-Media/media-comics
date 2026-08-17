# Changelog

## [v0.2.0] — 2026-08-10

### Added
- SQLite persistence (`comics.db` under data dir)
- Offline library root scan (path/filename metadata only)
- Scan fixture tests under `internal/testdata/library`
- SettingsProvider `data_dir` (read-only at runtime)
- HTTP JSON stubs `GET /api/series`, `GET /api/series/{id}`, `GET /api/issues` on health port

### Changed
- Replaced in-memory store with SQLite

## [v0.1.0] — 2026-08-10

### Added
- `ComicManagementService` (series/issues CRUD)
- In-memory library store
- SettingsProvider (`library_dir`)
- Health `:9661`
