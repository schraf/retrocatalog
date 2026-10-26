package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestExportStatic(t *testing.T) {
	game := testGame("igdb-1-nes", "Alpha", "nes", 1986, "Action")
	game.BoxArt = "https://images.igdb.com/igdb/image/upload/t_cover_big_2x/alpha.jpg"
	game.Screenshots = []string{
		"https://images.igdb.com/igdb/image/upload/t_screenshot_huge/one.jpg",
	}
	game.Overview = "First game."
	catalog, err := buildCatalog([]Game{game})
	if err != nil {
		t.Fatal(err)
	}

	webRoot, err := fs.Sub(webFiles, "web")
	if err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "site")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "stale.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := exportStatic(out, webRoot, catalog); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{
		"index.html",
		".nojekyll",
		"static/style.css",
		"static/app.js",
		"static/placeholder.svg",
		"data/catalog.json",
		"data/games/igdb-1-nes.json",
	} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "stale.txt")); !os.IsNotExist(err) {
		t.Errorf("stale.txt should have been removed, got %v", err)
	}

	data, err := os.ReadFile(filepath.Join(out, "data", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var response catalogResponse
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	if response.Total != 1 || len(response.Games) != 1 {
		t.Fatalf("catalog response = %#v", response)
	}
	if response.Games[0].BoxArt != game.BoxArt {
		t.Errorf("box_art = %q, want %q", response.Games[0].BoxArt, game.BoxArt)
	}
	if response.Games[0].ID != game.ID {
		t.Errorf("id = %q, want %q", response.Games[0].ID, game.ID)
	}

	data, err = os.ReadFile(filepath.Join(out, "data", "games", "igdb-1-nes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var detail Game
	if err := json.Unmarshal(data, &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Overview != game.Overview || len(detail.Screenshots) != 1 {
		t.Fatalf("detail = %#v", detail)
	}
	if detail.Screenshots[0] != game.Screenshots[0] {
		t.Errorf("screenshot = %q, want %q", detail.Screenshots[0], game.Screenshots[0])
	}
}

func TestExportStaticRefusesUnsafePaths(t *testing.T) {
	catalog, err := buildCatalog([]Game{testGame("igdb-1-nes", "Alpha", "nes", 1986, "Action")})
	if err != nil {
		t.Fatal(err)
	}
	webRoot, err := fs.Sub(webFiles, "web")
	if err != nil {
		t.Fatal(err)
	}

	for _, out := range []string{".", "..", "/", "../site"} {
		if err := exportStatic(out, webRoot, catalog); err == nil {
			t.Errorf("exportStatic(%q) = nil, want an error", out)
		}
	}
}

func TestExportStaticRefusesCatalogDirectory(t *testing.T) {
	catalog, err := buildCatalog([]Game{testGame("igdb-1-nes", "Alpha", "nes", 1986, "Action")})
	if err != nil {
		t.Fatal(err)
	}
	webRoot, err := fs.Sub(webFiles, "web")
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "catalog-v2.json"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := exportStatic(dir, webRoot, catalog); err == nil {
		t.Fatal("exportStatic(catalog dir) = nil, want an error")
	}
	if _, err := os.Stat(filepath.Join(dir, "catalog-v2.json")); err != nil {
		t.Fatalf("catalog snapshot was removed: %v", err)
	}
}
