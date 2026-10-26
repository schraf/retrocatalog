package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandleArtScreenshotIndexes(t *testing.T) {
	game := testGame("igdb-1-nes", "Alpha", "nes", 1986, "Action")
	game.Screenshots = []string{
		"https://images.igdb.com/igdb/image/upload/t_screenshot_huge/one.jpg",
		"https://images.igdb.com/igdb/image/upload/t_screenshot_huge/two.jpg",
		"https://images.igdb.com/igdb/image/upload/t_screenshot_huge/three.jpg",
	}
	catalog, err := buildCatalog([]Game{game})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	art, err := NewArtCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Pre-populate the per-slot cache so the handler never hits the network.
	cached := map[string]string{
		"shot-igdb-1-nes.jpg":  "zero",
		"shot1-igdb-1-nes.jpg": "one",
		"shot2-igdb-1-nes.jpg": "two",
	}
	for name, body := range cached {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	a := &app{catalog: catalog, art: art}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /art/{kind}/{id}", a.handleArt)

	tests := []struct {
		url    string
		status int
		body   string
	}{
		{"/art/shot/igdb-1-nes", http.StatusOK, "zero"},
		{"/art/shot/igdb-1-nes?i=0", http.StatusOK, "zero"},
		{"/art/shot/igdb-1-nes?i=1", http.StatusOK, "one"},
		{"/art/shot/igdb-1-nes?i=2", http.StatusOK, "two"},
		{"/art/shot/igdb-1-nes?i=3", http.StatusNotFound, ""},
		{"/art/shot/igdb-1-nes?i=-1", http.StatusBadRequest, ""},
		{"/art/shot/igdb-1-nes?i=abc", http.StatusBadRequest, ""},
	}
	for _, test := range tests {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, test.url, nil))
		if rec.Code != test.status {
			t.Errorf("%s: status = %d, want %d", test.url, rec.Code, test.status)
		}
		if test.body != "" && rec.Body.String() != test.body {
			t.Errorf("%s: body = %q, want %q", test.url, rec.Body.String(), test.body)
		}
	}
}
