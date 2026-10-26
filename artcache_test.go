package main

import (
	"bytes"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestImageExt(t *testing.T) {
	tests := []struct {
		contentType string
		want        string
		ok          bool
	}{
		{"image/jpeg", ".jpg", true},
		{"image/png", ".png", true},
		{"image/webp", ".webp", true},
		{"image/gif", ".gif", true},
		{"text/html", "", false},
		{"application/octet-stream", "", false},
	}
	for _, test := range tests {
		got, ok := imageExt(test.contentType)
		if got != test.want || ok != test.ok {
			t.Errorf("imageExt(%q) = %q, %v; want %q, %v", test.contentType, got, ok, test.want, test.ok)
		}
	}
}

func TestArtCacheFind(t *testing.T) {
	dir := t.TempDir()
	cache := &ArtCache{dir: dir}

	const id = "igdb-42-nes"
	if _, ok := cache.find(id); ok {
		t.Fatal("found art before it was cached")
	}

	path := filepath.Join(dir, id+".png")
	if err := os.WriteFile(path, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, ok := cache.find(id)
	if !ok || got != path {
		t.Fatalf("find(%q) = %q, %v; want %q, true", id, got, ok, path)
	}
	if _, ok := cache.find(id + "0"); ok {
		t.Fatalf("find(%q) should not match %s.png", id+"0", id)
	}
}

func TestArtCacheServesDownscaledVariant(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 800, 1200))
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, src, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}

	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(encoded.Bytes())
	}))
	defer server.Close()

	cache, err := NewArtCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	const id = "igdb-42-nes"
	serve := func(width int) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/art/box/"+id, nil)
		rec := httptest.NewRecorder()
		cache.Serve(rec, req, "box", id, server.URL+"/box.jpg", width)
		return rec
	}

	rec := serve(gridThumbWidth)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	out, format, err := image.Decode(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	if format != "jpeg" {
		t.Fatalf("format = %q, want jpeg", format)
	}
	if out.Bounds().Dx() != gridThumbWidth || out.Bounds().Dy() != 540 {
		t.Fatalf("size = %dx%d, want 360x540", out.Bounds().Dx(), out.Bounds().Dy())
	}
	if _, ok := cache.find("box-" + id + "-w360"); !ok {
		t.Fatal("thumbnail was not cached")
	}
	if _, ok := cache.find("box-" + id); !ok {
		t.Fatal("original was not cached")
	}

	rec = serve(gridThumbWidth)
	if rec.Code != http.StatusOK {
		t.Fatalf("cached status = %d, want 200", rec.Code)
	}
	if hits != 1 {
		t.Fatalf("upstream hits = %d, want 1", hits)
	}

	rec = serve(0)
	full, _, err := image.Decode(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	if full.Bounds().Dx() != 800 {
		t.Fatalf("full width = %d, want 800", full.Bounds().Dx())
	}
	if hits != 1 {
		t.Fatalf("upstream hits after full request = %d, want 1", hits)
	}
}

func TestArtCacheRejectsUnsupportedVariant(t *testing.T) {
	if !validThumbWidth(gridThumbWidth) || !validThumbWidth(panelThumbWidth) {
		t.Fatal("expected supported thumbnail widths")
	}
	if validThumbWidth(361) || validThumbWidth(0) {
		t.Fatal("unexpected width accepted")
	}
}

func TestValidateArtURL(t *testing.T) {
	valid := []string{
		"",
		"https://images.igdb.com/igdb/image/upload/t_cover_big_2x/co1234.jpg",
		"https://images.igdb.com/igdb/image/upload/t_screenshot_huge/sc1234.jpg",
	}
	for _, raw := range valid {
		if err := validateArtURL(raw); err != nil {
			t.Errorf("validateArtURL(%q) = %v", raw, err)
		}
	}

	invalid := []string{
		"http://images.igdb.com/igdb/image/upload/a.jpg",
		"https://example.com/a.jpg",
		"https://images.igdb.com/",
		"https://images.igdb.com",
		"not a url",
	}
	for _, raw := range invalid {
		if err := validateArtURL(raw); err == nil {
			t.Errorf("validateArtURL(%q) = nil, want error", raw)
		}
	}
}
