(() => {
  "use strict";

  const STORAGE_KEY = "nesdb.state.v2";
  const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" });
  const SORTS = ["name", "year", "top", "rating", "popular", "genre"];
  const DESCENDING_SORTS = new Set(["year", "top", "rating", "popular"]);
  // Must match the widths accepted by the Go art cache.
  const GRID_THUMB_WIDTH = 360;
  const PANEL_THUMB_WIDTH = 900;

  const el = {
    search: document.getElementById("search"),
    brandMark: document.getElementById("brand-mark"),
    platformChips: document.getElementById("platform-chips"),
    genreChips: document.getElementById("genre-chips"),
    yearMin: document.getElementById("year-min"),
    yearMax: document.getElementById("year-max"),
    rating: document.getElementById("rating-min"),
    ratingOutput: document.getElementById("rating-output"),
    sort: document.getElementById("sort"),
    sortDirection: document.getElementById("sort-direction"),
    favoritesToggle: document.getElementById("favorites-toggle"),
    favoritesCount: document.getElementById("favorites-count"),
    export: document.getElementById("export"),
    clearFilters: document.getElementById("clear-filters"),
    emptyClear: document.getElementById("empty-clear"),
    status: document.getElementById("status"),
    grid: document.getElementById("grid"),
    empty: document.getElementById("empty"),
    panel: document.getElementById("panel"),
    panelBody: document.querySelector(".panel-body"),
    backdrop: document.getElementById("backdrop"),
    closePanel: document.getElementById("close-panel"),
    prevGame: document.getElementById("prev-game"),
    nextGame: document.getElementById("next-game"),
    detailBox: document.getElementById("detail-box"),
    screenshots: document.getElementById("detail-screenshots"),
    detailShots: document.getElementById("detail-shots"),
    detailTitle: document.getElementById("detail-title"),
    detailSub: document.getElementById("detail-sub"),
    detailRating: document.getElementById("detail-rating"),
    detailFacts: document.getElementById("detail-facts"),
    detailTags: document.getElementById("detail-tags"),
    detailOverview: document.getElementById("detail-overview"),
    detailFavorite: document.getElementById("detail-favorite"),
    lightbox: document.getElementById("lightbox"),
    lightboxImage: document.getElementById("lightbox-image"),
    lightboxClose: document.getElementById("lightbox-close"),
    toast: document.getElementById("toast"),
  };

  let games = [];
  let byId = new Map();
  let favorites = new Set();
  let facets = null;
  let platforms = new Map();
  let visible = [];
  let selectedId = null;
  let selectedShots = [];
  let lightboxTrigger = null;
  let detailCache = new Map();
  let tiles = new Map();
  let highlightedTile = null;
  let renderQueued = false;
  let toastTimer = 0;

  const state = {
    query: "",
    platforms: new Set(),
    genres: new Set(),
    yearMin: 1985,
    yearMax: 1994,
    minRating: 0,
    favoritesOnly: false,
    sort: "name",
    dir: 1,
  };

  const saved = readSavedState();

  function clamp(value, min, max) {
    return Math.min(max, Math.max(min, value));
  }

  function readSavedState() {
    try {
      const raw = localStorage.getItem(STORAGE_KEY);
      return raw ? JSON.parse(raw) : null;
    } catch {
      return null;
    }
  }

  function saveState() {
    try {
      localStorage.setItem(
        STORAGE_KEY,
        JSON.stringify({
          query: state.query,
          platforms: [...state.platforms],
          genres: [...state.genres],
          yearMin: state.yearMin,
          yearMax: state.yearMax,
          minRating: state.minRating,
          favoritesOnly: state.favoritesOnly,
          sort: state.sort,
          dir: state.dir,
        }),
      );
    } catch {
      // Storage can be unavailable in private mode; the app still works.
    }
  }

  function applySavedState() {
    if (!saved || typeof saved !== "object") return;

    if (typeof saved.query === "string") {
      state.query = saved.query;
    }
    if (Array.isArray(saved.platforms)) {
      const known = new Set(facets.platforms.map((platform) => platform.slug));
      state.platforms = new Set(saved.platforms.filter((slug) => known.has(slug)));
    }
    if (Array.isArray(saved.genres)) {
      const known = new Set(facets.genres.map((genre) => genre.genre));
      state.genres = new Set(saved.genres.filter((genre) => known.has(genre)));
    }

    const yearMin = Number.isFinite(saved.yearMin) ? saved.yearMin : facets.year_min;
    const yearMax = Number.isFinite(saved.yearMax) ? saved.yearMax : facets.year_max;
    state.yearMin = clamp(Math.trunc(yearMin), facets.year_min, facets.year_max);
    state.yearMax = clamp(Math.trunc(yearMax), facets.year_min, facets.year_max);
    if (state.yearMin > state.yearMax) {
      [state.yearMin, state.yearMax] = [state.yearMax, state.yearMin];
    }

    if (Number.isFinite(saved.minRating)) {
      state.minRating = clamp(saved.minRating, 0, 5);
    }
    state.favoritesOnly = Boolean(saved.favoritesOnly);

    if (SORTS.includes(saved.sort)) {
      state.sort = saved.sort;
    }
    if (saved.dir === 1 || saved.dir === -1) {
      state.dir = saved.dir;
    }
  }

  function buildControls(data) {
    el.platformChips.replaceChildren();
    for (const facet of data.platforms) {
      const chip = document.createElement("button");
      chip.type = "button";
      chip.className = "chip";
      chip.dataset.platform = facet.slug;
      chip.setAttribute("aria-pressed", "false");
      chip.title = facet.name;

      const name = document.createElement("span");
      name.textContent = facet.short;

      const count = document.createElement("span");
      count.className = "chip-count";
      count.textContent = String(facet.count);

      chip.append(name, count);
      el.platformChips.append(chip);
    }

    el.genreChips.replaceChildren();
    for (const facet of data.genres) {
      const chip = document.createElement("button");
      chip.type = "button";
      chip.className = "chip";
      chip.dataset.genre = facet.genre;
      chip.setAttribute("aria-pressed", "false");

      const name = document.createElement("span");
      name.textContent = facet.genre;

      const count = document.createElement("span");
      count.className = "chip-count";
      count.textContent = String(facet.count);

      chip.append(name, count);
      el.genreChips.append(chip);
    }

    for (const select of [el.yearMin, el.yearMax]) {
      select.replaceChildren();
      for (let year = data.year_min; year <= data.year_max; year += 1) {
        const option = document.createElement("option");
        option.value = String(year);
        option.textContent = String(year);
        select.append(option);
      }
    }
  }

  function syncControls() {
    el.search.value = state.query;

    if (state.platforms.size === 1) {
      el.brandMark.textContent = platformShort([...state.platforms][0]);
      el.brandMark.classList.add("brand-mark-platform");
    } else {
      el.brandMark.textContent = "ALL";
      el.brandMark.classList.remove("brand-mark-platform");
    }

    for (const chip of el.platformChips.children) {
      const on = state.platforms.has(chip.dataset.platform);
      chip.classList.toggle("active", on);
      chip.setAttribute("aria-pressed", on ? "true" : "false");
    }

    for (const chip of el.genreChips.children) {
      const on = state.genres.has(chip.dataset.genre);
      chip.classList.toggle("active", on);
      chip.setAttribute("aria-pressed", on ? "true" : "false");
    }

    el.yearMin.value = String(state.yearMin);
    el.yearMax.value = String(state.yearMax);
    el.rating.value = String(state.minRating);
    el.ratingOutput.textContent = state.minRating > 0 ? `${state.minRating.toFixed(1)}+` : "Any";
    el.sort.value = state.sort;
    syncSortDirection();
    syncFavoriteUI();
  }

  function syncSortDirection() {
    el.sortDirection.textContent = state.dir === 1 ? "↑" : "↓";
    el.sortDirection.setAttribute("aria-label", state.dir === 1 ? "Sort ascending" : "Sort descending");
  }

  function syncFavoriteUI() {
    el.favoritesCount.textContent = String(favorites.size);
    el.favoritesToggle.classList.toggle("active", state.favoritesOnly);
    el.favoritesToggle.setAttribute("aria-pressed", state.favoritesOnly ? "true" : "false");
    updateFavoriteButton();
  }

  function bindEvents() {
    el.search.addEventListener("input", () => {
      state.query = el.search.value;
      scheduleRender();
    });

    el.platformChips.addEventListener("click", (event) => {
      const chip = event.target.closest(".chip");
      if (!chip) return;
      const platform = chip.dataset.platform;
      if (state.platforms.has(platform)) {
        state.platforms.delete(platform);
      } else {
        state.platforms.add(platform);
      }
      syncControls();
      render();
    });

    el.genreChips.addEventListener("click", (event) => {
      const chip = event.target.closest(".chip");
      if (!chip) return;
      const genre = chip.dataset.genre;
      if (state.genres.has(genre)) {
        state.genres.delete(genre);
      } else {
        state.genres.add(genre);
      }
      syncControls();
      render();
    });

    el.yearMin.addEventListener("change", () => {
      state.yearMin = Number(el.yearMin.value);
      if (state.yearMin > state.yearMax) {
        state.yearMax = state.yearMin;
      }
      syncControls();
      render();
    });

    el.yearMax.addEventListener("change", () => {
      state.yearMax = Number(el.yearMax.value);
      if (state.yearMax < state.yearMin) {
        state.yearMin = state.yearMax;
      }
      syncControls();
      render();
    });

    el.rating.addEventListener("input", () => {
      state.minRating = Number(el.rating.value);
      el.ratingOutput.textContent = state.minRating > 0 ? `${state.minRating.toFixed(1)}+` : "Any";
      scheduleRender();
    });

    el.sort.addEventListener("change", () => {
      state.sort = el.sort.value;
      state.dir = DESCENDING_SORTS.has(state.sort) ? -1 : 1;
      syncControls();
      render();
    });

    el.sortDirection.addEventListener("click", () => {
      state.dir *= -1;
      syncSortDirection();
      render();
    });

    el.clearFilters.addEventListener("click", clearFilters);
    el.emptyClear.addEventListener("click", clearFilters);

    el.favoritesToggle.addEventListener("click", () => {
      state.favoritesOnly = !state.favoritesOnly;
      syncControls();
      render();
    });

    el.export.addEventListener("click", (event) => {
      if (favorites.size === 0) {
        event.preventDefault();
        toast("No favorites to export yet.");
      }
    });

    el.grid.addEventListener("click", (event) => {
      const tile = event.target.closest(".tile");
      if (!tile) return;
      openPanel(tile.dataset.id);
    });

    // Image events do not bubble, so listen during the capture phase.
    el.grid.addEventListener(
      "load",
      (event) => {
        if (event.target instanceof HTMLImageElement) {
          event.target.classList.add("loaded");
        }
      },
      true,
    );
    el.grid.addEventListener(
      "error",
      (event) => {
        if (event.target instanceof HTMLImageElement) {
          fallbackImage(event.target);
        }
      },
      true,
    );

    el.closePanel.addEventListener("click", closePanel);
    el.backdrop.addEventListener("click", closePanel);
    el.prevGame.addEventListener("click", () => stepPanel(-1));
    el.nextGame.addEventListener("click", () => stepPanel(1));
    el.detailFavorite.addEventListener("click", () => {
      if (selectedId !== null) toggleFavorite(selectedId);
    });

    el.lightboxClose.addEventListener("click", closeLightbox);
    el.lightbox.addEventListener("click", (event) => {
      if (event.target === el.lightbox) closeLightbox();
    });

    document.addEventListener("keydown", onKeyDown);
  }

  function clearFilters() {
    state.query = "";
    state.platforms.clear();
    state.genres.clear();
    state.yearMin = facets.year_min;
    state.yearMax = facets.year_max;
    state.minRating = 0;
    state.favoritesOnly = false;
    syncControls();
    render();
  }

  function scheduleRender() {
    if (renderQueued) return;
    renderQueued = true;
    requestAnimationFrame(() => {
      renderQueued = false;
      render();
    });
  }

  function currentList() {
    const tokens = state.query.trim().toLowerCase().split(/\s+/).filter(Boolean);
    const minYear = state.yearMin;
    const maxYear = state.yearMax;
    const minRating = state.minRating;
    const onlyFavorites = state.favoritesOnly;

    const list = games.filter((game) => {
      if (onlyFavorites && !favorites.has(game.id)) return false;
      if (state.platforms.size > 0 && !state.platforms.has(game.platform)) return false;
      if (state.genres.size > 0 && !game.genres.some((genre) => state.genres.has(genre))) return false;
      if (game.year < minYear || game.year > maxYear) return false;
      if (game.rating < minRating) return false;
      for (const token of tokens) {
        if (!game._search.includes(token)) return false;
      }
      return true;
    });

    let primary;
    switch (state.sort) {
      case "year":
        primary = (a, b) => a.year - b.year;
        break;
      case "top":
        primary = (a, b) => a.weighted_rating - b.weighted_rating;
        break;
      case "rating":
        primary = (a, b) => a.rating - b.rating;
        break;
      case "popular":
        primary = (a, b) => a.rating_count - b.rating_count;
        break;
      case "genre":
        primary = (a, b) => collator.compare(a.genres[0] || "", b.genres[0] || "");
        break;
      default:
        primary = (a, b) => collator.compare(a.name, b.name);
    }
    list.sort((a, b) => {
      const order = primary(a, b);
      if (order !== 0) return order * state.dir;
      return collator.compare(a.name, b.name);
    });
    return list;
  }

  function render() {
    if (!facets) return;

    visible = currentList();
    const fragment = document.createDocumentFragment();

    for (const game of visible) {
      const tile = tileFor(game);
      tile.classList.toggle("favorite", favorites.has(game.id));
      tile.classList.toggle("selected", game.id === selectedId);
      fragment.append(tile);
    }

    el.grid.replaceChildren(fragment);
    el.grid.hidden = visible.length === 0;
    el.empty.hidden = visible.length !== 0;

    el.status.textContent = statusText();

    updatePanelNav();
    saveState();
  }

  function platformShort(slug) {
    const platform = platforms.get(slug);
    return platform ? platform.short : slug;
  }

  function platformName(slug) {
    const platform = platforms.get(slug);
    return platform ? platform.name : slug;
  }

  function tileFor(game) {
    let tile = tiles.get(game.id);
    if (tile) return tile;

    tile = document.createElement("button");
    tile.type = "button";
    tile.className = "tile";
    tile.dataset.id = game.id;
    tile.setAttribute("aria-label", `${game.name}, ${game.year}, ${platformName(game.platform)}`);

    const art = document.createElement("div");
    art.className = "tile-art";

    const image = document.createElement("img");
    image.loading = "lazy";
    image.decoding = "async";
    image.width = 300;
    image.height = 400;
    setImage(image, `/art/box/${game.id}?w=${GRID_THUMB_WIDTH}`, "");

    const platformBadge = document.createElement("span");
    platformBadge.className = "platform-badge";
    platformBadge.textContent = platformShort(game.platform);

    const badge = document.createElement("span");
    badge.className = "favorite-badge";
    badge.textContent = "★";
    badge.setAttribute("aria-hidden", "true");

    art.append(image, platformBadge, badge);

    const caption = document.createElement("div");
    caption.className = "tile-caption";

    const title = document.createElement("span");
    title.className = "tile-title";
    title.textContent = game.name;

    const year = document.createElement("span");
    year.className = "tile-year";
    year.textContent = `${game.year} · ${game.genres[0] || "Uncategorized"}`;

    caption.append(title, year);
    tile.append(art, caption);
    tiles.set(game.id, tile);
    return tile;
  }

  function setImage(image, src, alt) {
    image.classList.remove("loaded", "missing");
    delete image.dataset.fallback;
    image.alt = alt;
    image.onload = () => image.classList.add("loaded");
    image.onerror = () => fallbackImage(image);
    image.src = src;
    if (image.complete && image.naturalWidth > 0) {
      image.classList.add("loaded");
    }
  }

  function fallbackImage(image) {
    if (image.dataset.fallback) return;
    image.dataset.fallback = "1";
    image.onerror = null;
    image.src = "/static/placeholder.svg";
    image.classList.add("loaded", "missing");
  }

  function updatePanelNav() {
    const index = visible.findIndex((game) => game.id === selectedId);
    el.prevGame.disabled = index <= 0;
    el.nextGame.disabled = index < 0 || index >= visible.length - 1;
  }

  function markSelected() {
    const next = selectedId === null ? null : tiles.get(selectedId) || null;
    if (highlightedTile && highlightedTile !== next) {
      highlightedTile.classList.remove("selected");
    }
    highlightedTile = next;
    if (next) next.classList.add("selected");
  }

  function stepPanel(delta) {
    if (selectedId === null) return;
    const index = visible.findIndex((game) => game.id === selectedId);
    const next = visible[index + delta];
    if (next) openPanel(next.id, { focus: false });
  }

  async function openPanel(id, options = {}) {
    const game = byId.get(id);
    if (!game) return;

    selectedId = id;
    document.body.classList.add("panel-open");
    el.panel.classList.add("open");
    el.panel.inert = false;
    el.panel.setAttribute("aria-hidden", "false");
    el.backdrop.hidden = false;

    markSelected();
    fillPanelBasic(game);
    updatePanelNav();

    if (options.scroll !== false || options.focus !== false) {
      requestAnimationFrame(() => {
        if (options.scroll !== false) {
          el.panelBody.scrollTop = 0;
          // The reserved panel column reflows the grid; keep the chosen tile
          // in view when it moved past the fold.
          const tile = tiles.get(id);
          if (tile && typeof tile.scrollIntoView === "function") {
            tile.scrollIntoView({ block: "nearest", inline: "nearest" });
          }
        }
        if (options.focus !== false) el.panel.focus({ preventScroll: true });
      });
    }

    try {
      const detail = await getDetail(id);
      if (selectedId !== id) return;
      fillPanelDetail(detail);
    } catch (error) {
      if (selectedId !== id) return;
      console.error(error);
      el.detailOverview.textContent = "Could not load the full details.";
      el.detailOverview.classList.add("muted");
      toast("Could not load game details.");
    }
  }

  function closePanel() {
    const previous = selectedId;
    selectedId = null;
    selectedShots = [];
    el.panel.classList.remove("open");
    el.panel.inert = true;
    el.panel.setAttribute("aria-hidden", "true");
    el.backdrop.hidden = true;
    document.body.classList.remove("panel-open");
    markSelected();

    const tile = previous !== null ? tiles.get(previous) : null;
    if (tile) tile.focus({ preventScroll: true });
  }

  function playersLabel(maxPlayers) {
    if (!maxPlayers || maxPlayers < 1) return "";
    if (maxPlayers === 1) return "1 player";
    return `Up to ${maxPlayers} players`;
  }

  function appendFact(label, value) {
    if (!value) return;
    const term = document.createElement("dt");
    term.textContent = label;
    const detail = document.createElement("dd");
    detail.textContent = value;
    el.detailFacts.append(term, detail);
  }

  function stars(rating, scale) {
    const filled = clamp(Math.round((rating / (scale || 5)) * 5), 0, 5);
    return "★".repeat(filled) + "☆".repeat(5 - filled);
  }

  function renderRating(rating, ratingCount, scale) {
    el.detailRating.hidden = false;
    el.detailRating.replaceChildren();

    if (!ratingCount || rating <= 0) {
      const none = document.createElement("span");
      none.className = "muted";
      none.textContent = "No ratings";
      el.detailRating.append(none);
      return;
    }

    const starSpan = document.createElement("span");
    starSpan.className = "stars";
    starSpan.textContent = stars(rating, scale);

    const value = document.createElement("span");
    value.textContent = `${rating.toFixed(2)} / ${scale || 5}`;

    const count = document.createElement("span");
    count.className = "muted";
    count.textContent = `${ratingCount} ratings`;

    el.detailRating.append(starSpan, value, count);
  }

  function fillPanelBasic(game) {
    el.detailTitle.textContent = game.name;
    el.detailSub.textContent = `${game.year} · ${platformName(game.platform)}`;
    renderRating(game.rating, game.rating_count, 5);

    el.detailFacts.replaceChildren();
    appendFact("Platform", platformName(game.platform));
    appendFact("Publisher", game.publisher);
    appendFact("Developer", game.developer);
    appendFact("Players", playersLabel(game.max_players));
    if (game.cooperative) appendFact("Co-op", "Yes");
    appendFact("ESRB", game.esrb);

    el.detailTags.replaceChildren();
    for (const genre of game.genres || []) {
      const span = document.createElement("span");
      span.className = "tag";
      span.textContent = genre;
      el.detailTags.append(span);
    }
    el.detailOverview.textContent = "Loading details…";
    el.detailOverview.classList.add("muted");

    setImage(el.detailBox, `/art/box/${game.id}?w=${PANEL_THUMB_WIDTH}`, `${game.name} box art`);
    renderScreenshots(null);
    updateFavoriteButton();
  }

  function fillPanelDetail(game) {
    el.detailTitle.textContent = game.name;
    el.detailSub.textContent = `${game.release_year} · ${platformName(game.platform)}`;
    renderRating(game.rating, game.rating_count, game.rating_scale || 5);
    setImage(el.detailBox, `/art/box/${game.id}?w=${PANEL_THUMB_WIDTH}`, `${game.name} box art`);
    renderScreenshots(game);

    el.detailTags.replaceChildren();
    for (const genre of game.genres || []) {
      const span = document.createElement("span");
      span.className = "tag";
      span.textContent = genre;
      el.detailTags.append(span);
    }

    el.detailOverview.textContent = game.overview || "No overview available.";
    el.detailOverview.classList.toggle("muted", !game.overview);
    updateFavoriteButton();
  }

  function renderScreenshots(game) {
    selectedShots = (game && game.screenshots) || [];
    el.detailShots.replaceChildren();
    el.screenshots.hidden = selectedShots.length === 0;
    if (selectedShots.length === 0) return;

    selectedShots.forEach((_, index) => {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "screenshot-button";
      button.setAttribute("aria-label", `Enlarge screenshot ${index + 1} of ${selectedShots.length}`);

      const image = document.createElement("img");
      image.loading = "lazy";
      image.decoding = "async";
      setImage(
        image,
        `/art/shot/${game.id}?i=${index}&w=${PANEL_THUMB_WIDTH}`,
        `${game.name} screenshot ${index + 1}`,
      );

      button.append(image);
      button.addEventListener("click", () => openLightbox(index, button));
      el.detailShots.append(button);
    });
  }

  function updateFavoriteButton() {
    if (selectedId === null) return;
    const on = favorites.has(selectedId);
    el.detailFavorite.classList.toggle("active", on);
    el.detailFavorite.setAttribute("aria-pressed", on ? "true" : "false");
    el.detailFavorite.textContent = on ? "★ Favorited" : "☆ Add to favorites";
  }

  async function getDetail(id) {
    if (detailCache.has(id)) return detailCache.get(id);
    const response = await fetch(`/api/games/${id}`);
    if (!response.ok) {
      throw new Error(`game request failed (${response.status})`);
    }
    const detail = await response.json();
    detailCache.set(id, detail);
    return detail;
  }

  async function toggleFavorite(id) {
    const wasFavorite = favorites.has(id);
    applyFavorite(id, !wasFavorite);

    try {
      const response = await fetch(`/api/favorites/${id}`, {
        method: wasFavorite ? "DELETE" : "PUT",
      });
      if (!response.ok) {
        throw new Error(`favorites request failed (${response.status})`);
      }
      const data = await response.json();
      favorites = new Set(data.favorites);
    } catch (error) {
      console.error(error);
      applyFavorite(id, wasFavorite);
      toast("Could not update favorites.");
      return;
    }

    syncFavoriteUI();
    if (state.favoritesOnly && wasFavorite) {
      render();
    } else {
      el.status.textContent = statusText();
    }
  }

  function applyFavorite(id, on) {
    if (on) {
      favorites.add(id);
    } else {
      favorites.delete(id);
    }
    const tile = tiles.get(id);
    if (tile) tile.classList.toggle("favorite", on);
    syncFavoriteUI();
  }

  function statusText() {
    const parts = [`Showing ${visible.length} of ${games.length}`];
    if (favorites.size > 0) {
      parts.push(`${favorites.size} favorite${favorites.size === 1 ? "" : "s"}`);
    }
    if (state.sort === "top") {
      parts.push("weighted by ratings");
    }
    return parts.join(" · ");
  }

  function openLightbox(index, trigger) {
    if (selectedId === null || index < 0 || index >= selectedShots.length) return;
    const game = byId.get(selectedId);
    lightboxTrigger = trigger || null;
    delete el.lightboxImage.dataset.fallback;
    el.lightboxImage.onerror = () => fallbackImage(el.lightboxImage);
    el.lightboxImage.src = `/art/shot/${selectedId}?i=${index}`;
    el.lightboxImage.alt = `${game ? game.name : "Game"} screenshot ${index + 1}`;
    el.lightbox.hidden = false;
    el.lightboxClose.focus();
  }

  function closeLightbox() {
    el.lightbox.hidden = true;
    el.lightboxImage.removeAttribute("src");
    if (lightboxTrigger && document.contains(lightboxTrigger)) {
      lightboxTrigger.focus();
    } else if (selectedId !== null) {
      el.panel.focus({ preventScroll: true });
    }
    lightboxTrigger = null;
  }

  function onKeyDown(event) {
    if (event.key === "Escape") {
      if (!el.lightbox.hidden) {
        closeLightbox();
        return;
      }
      if (selectedId !== null) {
        closePanel();
        return;
      }
      if (isTyping(event.target)) {
        event.target.blur();
      }
      return;
    }

    if (!el.lightbox.hidden) return;
    if (isTyping(event.target)) return;

    if (event.key === "/") {
      event.preventDefault();
      el.search.focus();
      return;
    }

    if (event.key === "f" || event.key === "F") {
      state.favoritesOnly = !state.favoritesOnly;
      syncControls();
      render();
      return;
    }

    if (selectedId !== null) {
      if (event.key === "ArrowLeft") {
        event.preventDefault();
        stepPanel(-1);
      } else if (event.key === "ArrowRight") {
        event.preventDefault();
        stepPanel(1);
      }
    }
  }

  function isTyping(target) {
    return (
      target instanceof HTMLInputElement ||
      target instanceof HTMLSelectElement ||
      target instanceof HTMLTextAreaElement
    );
  }

  function toast(message) {
    el.toast.textContent = message;
    el.toast.hidden = false;
    window.clearTimeout(toastTimer);
    toastTimer = window.setTimeout(() => {
      el.toast.hidden = true;
    }, 2600);
  }

  async function boot() {
    try {
      const response = await fetch("/api/catalog", { headers: { Accept: "application/json" } });
      if (!response.ok) {
        throw new Error(`catalog request failed (${response.status})`);
      }
      const data = await response.json();

      games = data.games;
      byId = new Map(games.map((game) => [game.id, game]));
      favorites = new Set(data.favorites);
      facets = data;
      platforms = new Map(data.platforms.map((platform) => [platform.slug, platform]));

      for (const game of games) {
        game._search = `${game.name} ${game.publisher} ${game.developer} ${(game.genres || []).join(" ")} ${platformName(game.platform)}`.toLowerCase();
      }

      buildControls(data);
      applySavedState();
      syncControls();
      bindEvents();
      render();
    } catch (error) {
      console.error(error);
      el.status.textContent = `Could not load the catalog: ${error.message}`;
      el.status.classList.add("error");
    }
  }

  boot();
})();
