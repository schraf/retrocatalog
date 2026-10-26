package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFavoritesPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites.json")

	store, err := LoadFavorites(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"igdb-5-nes", "igdb-2-snes", "igdb-5-nes"} {
		if err := store.Add(id); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Remove("igdb-5-nes"); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove("igdb-99-nes"); err != nil {
		t.Fatal(err)
	}
	if err := store.Add("igdb-9-genesis"); err != nil {
		t.Fatal(err)
	}

	if got, want := store.List(), []string{"igdb-2-snes", "igdb-9-genesis"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("List() = %v, want %v", got, want)
	}
	if got, want := store.Export(), []string{"igdb-2-snes", "igdb-9-genesis"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Export() = %v, want %v", got, want)
	}

	reloaded, err := LoadFavorites(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Has("igdb-2-snes") || !reloaded.Has("igdb-9-genesis") || reloaded.Has("igdb-5-nes") {
		t.Fatalf("reloaded favorites = %v", reloaded.List())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var payload favoritesFile
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("favorites file is not valid JSON: %v", err)
	}
	if payload.Version != 2 || payload.UpdatedAt.IsZero() {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestFavoritesExportSorted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites.json")
	store, err := LoadFavorites(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"igdb-9-nes", "igdb-2-snes", "igdb-5-genesis"} {
		if err := store.Add(id); err != nil {
			t.Fatal(err)
		}
	}

	if got, want := store.Export(), []string{"igdb-2-snes", "igdb-5-genesis", "igdb-9-nes"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Export() = %v, want %v", got, want)
	}
}

func TestFavoritesEmptyIsArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites.json")

	store, err := LoadFavorites(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add("igdb-1-nes"); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove("igdb-1-nes"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var payload favoritesFile
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Favorites == nil {
		t.Fatalf("empty favorites encoded as null: %s", data)
	}
	if len(payload.Favorites) != 0 {
		t.Fatalf("favorites = %v, want empty", payload.Favorites)
	}
}

func TestLoadFavoritesBareArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites.json")
	if err := os.WriteFile(path, []byte("[\n  \"igdb-9-nes\",\n  \"igdb-2-snes\",\n  \"igdb-9-nes\",\n  \"\"\n]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := LoadFavorites(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := store.List(), []string{"igdb-9-nes", "igdb-2-snes"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("List() = %v, want %v", got, want)
	}

	// Touching the store rewrites it in the canonical envelope format.
	if err := store.Add("igdb-4-tg16"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadFavorites(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := reloaded.List(), []string{"igdb-9-nes", "igdb-2-snes", "igdb-4-tg16"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("reloaded List() = %v, want %v", got, want)
	}
}

func TestLoadFavoritesDiscardsLegacyNumericIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites.json")
	if err := os.WriteFile(path, []byte("[\n  9,\n  2\n]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := LoadFavorites(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.List(); len(got) != 0 {
		t.Fatalf("List() = %v, want empty", got)
	}

	envelope := filepath.Join(t.TempDir(), "favorites.json")
	if err := os.WriteFile(envelope, []byte("{\"version\":1,\"favorites\":[9,2]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err = LoadFavorites(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.List(); len(got) != 0 {
		t.Fatalf("envelope List() = %v, want empty", got)
	}
}

func TestLoadFavoritesMissingFile(t *testing.T) {
	store, err := LoadFavorites(filepath.Join(t.TempDir(), "nested", "favorites.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := store.List(); len(got) != 0 {
		t.Fatalf("List() = %v, want empty", got)
	}
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")

	if err := writeFileAtomic(path, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "second" {
		t.Fatalf("content = %q, want second", data)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("leftover temporary files: %v", entries)
	}
}
