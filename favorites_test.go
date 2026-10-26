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
	if err := store.Add(5); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(2); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(5); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove(5); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove(99); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(9); err != nil {
		t.Fatal(err)
	}

	if got, want := store.List(), []int{2, 9}; !reflect.DeepEqual(got, want) {
		t.Fatalf("List() = %v, want %v", got, want)
	}
	if got, want := store.Export(), []int{2, 9}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Export() = %v, want %v", got, want)
	}

	reloaded, err := LoadFavorites(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Has(2) || !reloaded.Has(9) || reloaded.Has(5) {
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
	if payload.Version != 1 || payload.UpdatedAt.IsZero() {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestFavoritesEmptyIsArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites.json")

	store, err := LoadFavorites(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add(1); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove(1); err != nil {
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
	if err := os.WriteFile(path, []byte("[\n  9,\n  2,\n  9,\n  0\n]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := LoadFavorites(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := store.List(), []int{9, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("List() = %v, want %v", got, want)
	}

	// Touching the store rewrites it in the canonical envelope format.
	if err := store.Add(4); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadFavorites(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := reloaded.List(), []int{9, 2, 4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("reloaded List() = %v, want %v", got, want)
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
