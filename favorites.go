package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type favoritesFile struct {
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	Favorites []int     `json:"favorites"`
}

// Favorites is a small JSON-backed set with atomic persistence.
type Favorites struct {
	path string

	mu    sync.RWMutex
	ids   map[int]bool
	order []int
}

// LoadFavorites reads favorites.json. A missing file is treated as empty.
func LoadFavorites(path string) (*Favorites, error) {
	f := &Favorites{
		path: path,
		ids:  make(map[int]bool),
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read favorites: %w", err)
	}

	// Accept both the stored envelope and the bare ascending array produced by
	// the export endpoint, so saving an export over favorites.json still works.
	var ids []int
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '[' {
		if err := json.Unmarshal(data, &ids); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	} else {
		var payload favoritesFile
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		ids = payload.Favorites
	}
	for _, id := range ids {
		if id <= 0 || f.ids[id] {
			continue
		}
		f.ids[id] = true
		f.order = append(f.order, id)
	}
	return f, nil
}

// List returns favorites in the order they were added.
func (f *Favorites) List() []int {
	f.mu.RLock()
	defer f.mu.RUnlock()

	out := make([]int, len(f.order))
	copy(out, f.order)
	return out
}

// Export returns the favorite IDs sorted ascending.
func (f *Favorites) Export() []int {
	ids := f.List()
	sort.Ints(ids)
	return ids
}

// Has reports whether a game is a favorite.
func (f *Favorites) Has(id int) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.ids[id]
}

// Add marks a game as a favorite and persists the change.
func (f *Favorites) Add(id int) error {
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
func (f *Favorites) Remove(id int) error {
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
	favorites := make([]int, len(f.order))
	copy(favorites, f.order)

	payload := favoritesFile{
		Version:   1,
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
