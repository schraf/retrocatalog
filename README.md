# nesdb

A small, self-hosted catalog browser for four retro platforms:

- Nintendo Entertainment System
- Super Nintendo Entertainment System
- Sega Genesis
- TurboGrafx-16

Game metadata comes from the [IGDB API](https://api-docs.igdb.com/). The
normalized catalog is cached locally so the app runs offline once it has been
fetched.

## Running

```sh
export IGDB_CLIENT_ID=...
export IGDB_CLIENT_SECRET=...
go run .
```

Then open <http://127.0.0.1:8080>.

Credentials are only needed when the catalog cache is missing or stale. A fresh
cache starts without network access.

The app also loads a `.env` file from the working directory when present:

```sh
# .env
IGDB_CLIENT_ID=...
IGDB_CLIENT_SECRET=...
```

Existing environment variables take precedence over values from `.env`, and the
file is ignored by git.

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-addr` | `127.0.0.1:8080` | Listen address |
| `-cache` | `.cache` | Catalog and art cache directory |
| `-favorites` | `favorites.json` | Favorites persistence file |
| `-refresh` | `false` | Ignore a fresh cache and fetch from IGDB |

## Catalog cache

The catalog is stored as a plain `[]Game` array in
`.cache/catalog-v2.json`. Its modification time is the cache timestamp:

- younger than 24 hours: used as-is
- older, or `-refresh`: refetched from IGDB
- refetch fails: a valid stale cache is served with a warning
- no valid cache and the fetch fails: startup fails with an error

Writes are atomic. Delete the cache file to force a refetch. Bump the filename
in `igdb.go` when the schema or filtering rules change.

The cache is intentionally not committed. Favorites and downloaded art are
local state as well.

## Catalog rules

- One tile per platform release, with ID `igdb-<gameID>-<platform>`.
- Only `main_game` and `port` entries. Records marked Released, or with no
  IGDB status, are included; other statuses are excluded.
- A North American release is preferred; a Worldwide release is used as a
  fallback. The earliest eligible date supplies the year.
- Canceled and rumored release dates are ignored.
- IGDB ratings (0–100) are converted to 0–5.
- Duplicate `(platform, normalized title)` entries are merged, preferring a
  `main_game`, then more ratings, then a higher rating, then the lower IGDB ID.
- Up to three IGDB screenshots are kept per game. The detail panel shows them
  as thumbnails and opens the full-size image in the lightbox.
- Art URLs are restricted to `images.igdb.com` and proxied through the local
  art cache.

## Tests

```sh
go test ./...
go vet ./...
```

Tests use `httptest`; no IGDB credentials or network access are required.
