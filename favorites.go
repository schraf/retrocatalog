package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type favoritesFile struct {
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	Favorites []string  `json:"favorites"`
}

// Favorites is a small JSON-backed set with atomic persistence.
type Favorites struct {
	path string

	mu    sync.RWMutex
	ids   map[string]bool
	order []string
}

// LoadFavorites reads favorites.json. A missing file is treated as empty.
// Legacy numeric IDs from the NES-only catalog are discarded.
func LoadFavorites(path string) (*Favorites, error) {
	f := &Favorites{
		path: path,
		ids:  make(map[string]bool),
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read favorites: %w", err)
	}

	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return f, nil
	}

	var ids []string
	if data[0] == '[' {
		ids, err = parseFavoriteIDs(data)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	} else {
		var envelope struct {
			Version   int             `json:"version"`
			Favorites json.RawMessage `json:"favorites"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		ids, err = parseFavoriteIDs(envelope.Favorites)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	}

	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || f.ids[id] {
			continue
		}
		f.ids[id] = true
		f.order = append(f.order, id)
	}
	return f, nil
}

func parseFavoriteIDs(data []byte) ([]string, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}

	var ids []string
	if err := json.Unmarshal(data, &ids); err == nil {
		return ids, nil
	}

	var legacy []int
	if err := json.Unmarshal(data, &legacy); err == nil {
		log.Printf("favorites: ignoring legacy numeric IDs; favorites reset")
		return nil, nil
	}
	return nil, fmt.Errorf("expected a string array")
}

// List returns favorites in the order they were added.
func (f *Favorites) List() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	out := make([]string, len(f.order))
	copy(out, f.order)
	return out
}

// Export returns the favorite IDs sorted ascending.
func (f *Favorites) Export() []string {
	ids := f.List()
	sort.Strings(ids)
	return ids
}

// Has reports whether a game is a favorite.
func (f *Favorites) Has(id string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.ids[id]
}

// Add marks a game as a favorite and persists the change.
func (f *Favorites) Add(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.ids[id] {
		return nil
	}
	f.ids[id] = true
	f.order = append(f.order, id)
	return f.saveLocked()
}

// Remove unmarks a game and persists the change.
func (f *Favorites) Remove(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.ids[id] {
		return nil
	}
	delete(f.ids, id)
	for i, existing := range f.order {
		if existing == id {
			f.order = append(f.order[:i], f.order[i+1:]...)
			break
		}
	}
	return f.saveLocked()
}

func (f *Favorites) saveLocked() error {
	favorites := make([]string, len(f.order))
	copy(favorites, f.order)

	payload := favoritesFile{
		Version:   2,
		UpdatedAt: time.Now().UTC(),
		Favorites: favorites,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(f.path, data, 0o644)
}

// writeFileAtomic writes data to a temporary file in the target directory and
// renames it into place, so readers never observe a partial file.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	tmpName = ""
	return nil
}
