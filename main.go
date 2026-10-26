package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

//go:embed web
var webFiles embed.FS

type app struct {
	catalog   *Catalog
	favorites *Favorites
	art       *ArtCache
	indexHTML []byte
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "address to listen on")
	dataPath := flag.String("data", "games.json", "catalog JSON file")
	cacheDir := flag.String("cache", ".cache/art", "directory for cached art")
	favoritesPath := flag.String("favorites", "favorites.json", "file used to persist favorites")
	flag.Parse()

	catalog, err := LoadCatalog(*dataPath)
	if err != nil {
		log.Fatal(err)
	}
	favorites, err := LoadFavorites(*favoritesPath)
	if err != nil {
		log.Fatal(err)
	}
	art, err := NewArtCache(*cacheDir)
	if err != nil {
		log.Fatal(err)
	}

	webRoot, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatal(err)
	}
	indexHTML, err := fs.ReadFile(webRoot, "index.html")
	if err != nil {
		log.Fatal(err)
	}

	a := &app{
		catalog:   catalog,
		favorites: favorites,
		art:       art,
		indexHTML: indexHTML,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.handleIndex)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(webRoot)))
	mux.HandleFunc("GET /api/catalog", a.handleCatalog)
	mux.HandleFunc("GET /api/games/{id}", a.handleGame)
	mux.HandleFunc("GET /art/{kind}/{id}", a.handleArt)
	mux.HandleFunc("GET /api/favorites", a.handleFavorites)
	mux.HandleFunc("PUT /api/favorites/{id}", a.handleFavoritePut)
	mux.HandleFunc("DELETE /api/favorites/{id}", a.handleFavoriteDelete)
	mux.HandleFunc("GET /api/favorites/export", a.handleExport)

	server := &http.Server{
		Addr:              *addr,
		Handler:           securityHeaders(logRequests(mux)),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("loaded %d games from %s (%d genres, %d favorites)",
			catalog.Total(), *dataPath, len(catalog.Genres()), len(favorites.List()))
		log.Printf("listening on http://%s", *addr)
		errCh <- server.ListenAndServe()
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case <-ctx.Done():
		log.Printf("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}
}

func (a *app) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(a.indexHTML)
}

type catalogResponse struct {
	Games     []IndexGame  `json:"games"`
	Favorites []int        `json:"favorites"`
	Genres    []GenreCount `json:"genres"`
	YearMin   int          `json:"year_min"`
	YearMax   int          `json:"year_max"`
	RatingMin float64      `json:"rating_min"`
	RatingMax float64      `json:"rating_max"`
	Total     int          `json:"total"`
}

func (a *app) handleCatalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, catalogResponse{
		Games:     a.catalog.Index(),
		Favorites: a.favorites.List(),
		Genres:    a.catalog.Genres(),
		YearMin:   a.catalog.yearMin,
		YearMax:   a.catalog.yearMax,
		RatingMin: a.catalog.ratingMin,
		RatingMax: a.catalog.ratingMax,
		Total:     a.catalog.Total(),
	})
}

func (a *app) handleGame(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	game := a.catalog.ByID(id)
	if game == nil {
		writeError(w, http.StatusNotFound, "game not found")
		return
	}
	writeJSON(w, http.StatusOK, game)
}

func (a *app) handleArt(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	game := a.catalog.ByID(id)
	if game == nil {
		http.NotFound(w, r)
		return
	}

	kind := r.PathValue("kind")
	var source string
	switch kind {
	case "box":
		source = game.BoxArt
	case "shot":
		source = game.Screenshot
	default:
		http.NotFound(w, r)
		return
	}
	if source == "" {
		http.NotFound(w, r)
		return
	}

	width := 0
	if raw := r.URL.Query().Get("w"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || !validThumbWidth(n) {
			writeError(w, http.StatusBadRequest, "invalid width")
			return
		}
		width = n
	}

	a.art.Serve(w, r, kind, id, source, width)
}

func (a *app) handleFavorites(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string][]int{"favorites": a.favorites.List()})
}

func (a *app) handleFavoritePut(w http.ResponseWriter, r *http.Request) {
	a.mutateFavorite(w, r, true)
}

func (a *app) handleFavoriteDelete(w http.ResponseWriter, r *http.Request) {
	a.mutateFavorite(w, r, false)
}

func (a *app) mutateFavorite(w http.ResponseWriter, r *http.Request, add bool) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if a.catalog.ByID(id) == nil {
		writeError(w, http.StatusNotFound, "game not found")
		return
	}

	var err error
	if add {
		err = a.favorites.Add(id)
	} else {
		err = a.favorites.Remove(id)
	}
	if err != nil {
		log.Printf("save favorites: %v", err)
		writeError(w, http.StatusInternalServerError, "could not save favorites")
		return
	}
	writeJSON(w, http.StatusOK, map[string][]int{"favorites": a.favorites.List()})
}

func (a *app) handleExport(w http.ResponseWriter, r *http.Request) {
	data, err := json.MarshalIndent(a.favorites.Export(), "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not build export")
		return
	}
	data = append(data, '\n')

	name := fmt.Sprintf("nesdb-favorites-%s.json", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func pathID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid game id")
		return 0, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; style-src 'self'; "+
				"script-src 'self'; connect-src 'self'; base-uri 'none'; "+
				"form-action 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, sw.status, time.Since(start).Round(time.Millisecond))
	})
}
