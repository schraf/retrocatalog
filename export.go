package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// catalogResponse is the JSON shape consumed by the static frontend.
type catalogResponse struct {
	Games     []IndexGame     `json:"games"`
	Genres    []GenreCount    `json:"genres"`
	Platforms []PlatformCount `json:"platforms"`
	YearMin   int             `json:"year_min"`
	YearMax   int             `json:"year_max"`
	RatingMin float64         `json:"rating_min"`
	RatingMax float64         `json:"rating_max"`
	Total     int             `json:"total"`
}

// exportStatic writes a self-contained static site to outDir.
func exportStatic(outDir string, webRoot fs.FS, catalog *Catalog) error {
	outDir = filepath.Clean(outDir)
	if outDir == "." || outDir == ".." || outDir == string(filepath.Separator) {
		return fmt.Errorf("refusing to export into %q", outDir)
	}
	if strings.HasPrefix(outDir, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to export outside the working directory: %q", outDir)
	}
	if _, err := os.Stat(filepath.Join(outDir, ".git")); err == nil {
		return fmt.Errorf("refusing to replace %s: it contains a .git directory", outDir)
	}
	if _, err := os.Stat(filepath.Join(outDir, "catalog-v3.json")); err == nil {
		return fmt.Errorf("refusing to replace %s: it contains the catalog snapshot", outDir)
	}

	if err := os.RemoveAll(outDir); err != nil {
		return fmt.Errorf("clear %s: %w", outDir, err)
	}
	for _, dir := range []string{
		filepath.Join(outDir, "static"),
		filepath.Join(outDir, "data", "games"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	if err := copyWebFiles(webRoot, outDir); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, ".nojekyll"), nil, 0o644); err != nil {
		return fmt.Errorf("write .nojekyll: %w", err)
	}

	response := catalogResponse{
		Games:     catalog.Index(),
		Genres:    catalog.Genres(),
		Platforms: catalog.Platforms(),
		YearMin:   catalog.yearMin,
		YearMax:   catalog.yearMax,
		RatingMin: catalog.ratingMin,
		RatingMax: catalog.ratingMax,
		Total:     catalog.Total(),
	}
	if err := writeJSONFile(filepath.Join(outDir, "data", "catalog.json"), response); err != nil {
		return err
	}
	for _, game := range catalog.games {
		path := filepath.Join(outDir, "data", "games", game.ID+".json")
		if err := writeJSONFile(path, game); err != nil {
			return err
		}
	}
	return nil
}

func copyWebFiles(webRoot fs.FS, outDir string) error {
	entries, err := fs.ReadDir(webRoot, ".")
	if err != nil {
		return fmt.Errorf("read embedded web files: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := fs.ReadFile(webRoot, entry.Name())
		if err != nil {
			return fmt.Errorf("read web/%s: %w", entry.Name(), err)
		}
		dest := filepath.Join(outDir, "static", entry.Name())
		if entry.Name() == "index.html" {
			dest = filepath.Join(outDir, "index.html")
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dest, err)
		}
	}
	return nil
}

func writeJSONFile(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
