# retrocatalog

A small static catalog browser for seven Nintendo platforms:

- Nintendo Entertainment System
- Game Boy
- Super Nintendo Entertainment System
- Nintendo 64
- Game Boy Color
- Game Boy Advance
- Nintendo GameCube

Game metadata and artwork come from the [IGDB API](https://api-docs.igdb.com/).
Community ratings come primarily from the [LaunchBox Games Database](https://gamesdb.launchbox-app.com/),
with IGDB ratings as a fallback. The catalog is normalized once, committed as a
snapshot, and rendered as a self-contained static site. Favorites live in the
browser; there is no backend.

## Building

```sh
go run .
```

This reads `data/catalog-v3.json` and writes the site to `dist/`. It needs no
network access or IGDB credentials.

Preview it locally:

```sh
python3 -m http.server -d dist
```

Then open <http://127.0.0.1:8000>. The app fetches JSON, so open it through
a local web server rather than directly from `file://`.

| Flag | Default | Description |
| --- | --- | --- |
| `-cache` | `data` | Directory containing `catalog-v3.json` |
| `-export` | `dist` | Directory to write the static site |
| `-refresh` | `false` | Ignore the snapshot and fetch from IGDB and LaunchBox |
| `-launchbox-metadata` | none | Local LaunchBox `Metadata.xml` or `.zip`; downloads the current export when empty |

## Refreshing the snapshot

The catalog changes rarely, so refreshes are manual. Set IGDB credentials in the
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

`-refresh` fetches the IGDB catalog, downloads the LaunchBox bulk metadata
export (about 100 MB compressed), merges ratings, writes `data/catalog-v3.json`,
and rebuilds `dist/`. To reuse an already-downloaded export, pass it directly
instead of downloading it again:

```sh
go run . -refresh -launchbox-metadata ~/Metadata.zip
```

Commit the snapshot to publish the new data:

```sh
git add data/catalog-v3.json
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
- Only `main_game`, `expanded_game`, and `port` entries. Records marked
  Released, or with no IGDB status, are included; other statuses are excluded.
- Releases dated after a platform's commercial lifespan are treated as homebrew,
  bootlegs, or modern re-releases and excluded. The cutoffs are NES 1994, Game
  Boy 1999, SNES 1998, Nintendo 64 2002, Game Boy Color 2003, Game Boy Advance
  2008, and GameCube 2007.
- Entries without cover art are excluded. Screenshots are optional.
- A North American release is preferred; a Worldwide release is used as a
  fallback. The earliest eligible date supplies the year.
- Canceled and rumored release dates are ignored.
- IGDB ratings (0–100) are converted to 0–5.
- Community ratings are merged from LaunchBox first. LaunchBox records marked
  `Unlicensed`, `Homebrew`, `ROM Hack`, or `Unreleased` are skipped; a missing
  release type is treated as released. LaunchBox is preferred when it has at
  least five votes; otherwise a qualifying IGDB score is used. When neither
  source reaches five votes, the one with any votes supplies the score, and
  games with no votes stay in the catalog as unrated.
- The detail panel shows the selected source and, when both exist, the raw
  LaunchBox and IGDB scores and vote counts. The grid index also carries the
  selected source.
- Duplicate `(platform, normalized title)` entries are merged, preferring a
  `main_game`, then more ratings, then a higher rating, then the lower IGDB ID.
- Compilation sub-games (Action 52 and Kirby Super Star), promotional or
  competition cartridges, and non-game software are excluded by platform and
  normalized title, not by whether they have a rating.
- Up to three IGDB screenshots are kept per game. The detail panel shows them
  as thumbnails and opens the full-size image in the lightbox.
- Art URLs are restricted to `images.igdb.com`. Cover art and screenshots are
  hotlinked from the IGDB image CDN; no image files are stored in this
  repository.

## Ranking

Rating-based views use a Bayesian weighted rating with a prior of 25 votes
(`(votes × rating + 25 × platform mean) / (votes + 25)`). The prior is computed
per platform so that, for example, GameCube scores are not pulled toward the NES
average. Unrated games are kept and sort last in every rating-based view; they
are never treated as average-rated or included in the top results.

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

Tests use `httptest` for the IGDB client and a small LaunchBox XML fixture; no
credentials or network access are required.

## Attribution

Game data and images are provided by [IGDB.com](https://www.igdb.com/). Community
ratings come from the [LaunchBox Games Database](https://gamesdb.launchbox-app.com/).
This project is not affiliated with IGDB, Twitch, LaunchBox, or Nintendo. All
game titles, cover art, and screenshots belong to their respective owners.

## License

MIT. See [LICENSE](LICENSE).
