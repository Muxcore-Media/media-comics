# Media Comics

Manga / comic **series + issue** library manager scaffold for MuxCore.

Exposes `muxcore.comics.v1.ComicManagementService` with an in-memory store in **v0.1.0**, plus SettingsProvider for `library_dir`.

## Ports

| Service | Default |
|---------|---------|
| gRPC | `:9660` |
| Health | `:9661` |

## Build / test

```bash
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go build -o bin/media-comics ./cmd/module
```

## Status

v0.1.0 scaffold — optional peer. Persistence, ComicVine/AniList search, and acquisition wiring are follow-ups.
