# retrocatalog

A small static catalog browser for four retro platforms:

- Nintendo Entertainment System
- Super Nintendo Entertainment System
- Sega Genesis
- TurboGrafx-16

Game metadata and artwork come from the [IGDB API](https://api-docs.igdb.com/).
The catalog is normalized once, committed as a snapshot, and rendered as a
self-contained static site. Favorites live in the browser; there is no backend.

## Building

```sh
go run .
```

This reads `data/catalog-v2.json` and writes the site to `dist/`. It needs no
network access or IGDB credentials.

Preview it locally:

```sh
python3 -m http.server -d dist
```

Then open <http://127.0.0.1:8000>. The app fetches JSON, so open it through
a local web server rather than directly from `file://`.

| Flag | Default | Description |
| --- | --- | --- |
| `-cache` | `data` | Directory containing `catalog-v2.json` |
| `-export` | `dist` | Directory to write the static site |
| `-refresh` | `false` | Ignore the snapshot and fetch from IGDB |

## Refreshing the snapshot

The catalog changes rarely, so refreshes are manual. Set credentials in the
environment or in a local `.env` file:

```sh
# .env
IGDB_CLIENT_ID=...
IGDB_CLIENT_SECRET=...
```

Then run:

```sh
go run . -refresh
```

`-refresh` fetches from IGDB, normalizes the data, writes
`data/catalog-v2.json`, and rebuilds `dist/`. Commit the snapshot to publish the
new data:

```sh
git add data/catalog-v2.json
git commit -m "Refresh catalog snapshot"
```

The snapshot is committed so builds and contributors do not need credentials.
Existing environment variables take precedence over `.env`; the file is ignored
by git.

## Static output

```text
dist/
  index.html
  .nojekyll
  static/
    style.css
    app.js
    placeholder.svg
  data/
    catalog.json        # grid index and filter facets
    games/<id>.json     # full detail records, loaded on demand
```

The generated `dist/` directory is ignored by git. GitHub Actions builds it and
uploads it as a Pages artifact.

## Catalog rules

- One tile per platform release, with ID `igdb-<gameID>-<platform>`.
- Only `main_game` and `port` entries. Records marked Released, or with no
  IGDB status, are included; other statuses are excluded.
- Releases dated after a platform's commercial lifespan are treated as homebrew,
  bootlegs, or modern re-releases and excluded: NES and TG16 through 1994, SNES
  through 1998, Genesis through 1997.
- Entries without cover art are excluded. Screenshots are optional.
- A North American release is preferred; a Worldwide release is used as a
  fallback. The earliest eligible date supplies the year.
- Canceled and rumored release dates are ignored.
- IGDB ratings (0–100) are converted to 0–5.
- Duplicate `(platform, normalized title)` entries are merged, preferring a
  `main_game`, then more ratings, then a higher rating, then the lower IGDB ID.
- Up to three IGDB screenshots are kept per game. The detail panel shows them
  as thumbnails and opens the full-size image in the lightbox.
- Art URLs are restricted to `images.igdb.com`. Cover art and screenshots are
  hotlinked from the IGDB image CDN; no image files are stored in this
  repository.

## Favorites

Favorites are stored in `localStorage` under `retrocatalog.favorites.v2`. They
are per browser and per device; clearing site data removes them.

- **Export** downloads a JSON file with the same format as the old server:
  `{ "version": 2, "updated_at": "...", "favorites": [...] }`.
- **Import** accepts that format or a plain JSON string array, merges the IDs
  into the current list, and ignores unknown games. To carry over favorites from
  the old local server, select its `favorites.json` file.
- Filter state is stored separately under `retrocatalog.state.v2`.

The keys are namespaced because all `*.github.io` project sites share one
`localStorage` origin.

## Deployment

The workflow in `.github/workflows/pages.yml` runs on pushes to `main` and on
manual dispatch. It runs the tests, builds `dist/`, and deploys it with the
GitHub Pages Actions flow.

In the repository settings, set **Pages → Build and deployment → Source** to
**GitHub Actions**. The site is published at
`https://<user>.github.io/<repo>/`; all asset and data paths are relative, so
project sites work without a custom domain.

The workflow needs no IGDB secrets because it builds from the committed
snapshot. GitHub Pages is available for public repositories on the free plan.

## Tests

```sh
go test ./...
go vet ./...
```

Tests use `httptest` for the IGDB client; no credentials or network access are
required.

## Attribution

Game data and images are provided by [IGDB.com](https://www.igdb.com/). This
project is not affiliated with IGDB, Twitch, Nintendo, Sega, or NEC. All game
titles, cover art, and screenshots belong to their respective owners.

## License

MIT. See [LICENSE](LICENSE).
