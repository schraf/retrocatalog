package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxArtBytes = 15 << 20 // 15 MB
	failureTTL  = 5 * time.Minute
	maxParallel = 6
)

// Thumbnail widths the browser may request. The full-size image is used for
// the lightbox; tiles and the detail panel use cached downscaled variants.
const (
	gridThumbWidth  = 360
	panelThumbWidth = 900
)

func validThumbWidth(width int) bool {
	return width == gridThumbWidth || width == panelThumbWidth
}

var knownExts = []string{".jpg", ".png", ".webp", ".gif"}

// ArtCache downloads box art and screenshots on demand and keeps them on disk.
type ArtCache struct {
	dir    string
	client *http.Client
	sem    chan struct{}

	mu     sync.Mutex
	locks  map[string]*sync.Mutex
	failed map[string]time.Time
}

// NewArtCache prepares the cache directory.
func NewArtCache(dir string) (*ArtCache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create art cache: %w", err)
	}

	return &ArtCache{
		dir: dir,
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("too many redirects")
				}
				if !allowedArtHosts[req.URL.Hostname()] {
					return fmt.Errorf("redirect to disallowed host %q", req.URL.Hostname())
				}
				return nil
			},
		},
		sem:    make(chan struct{}, maxParallel),
		locks:  make(map[string]*sync.Mutex),
		failed: make(map[string]time.Time),
	}, nil
}

// Serve streams the cached image, downloading it first when needed. Kind is
// "box", "shot", or a gallery slot such as "shot1"; each kind is cached
// separately for the same game. A non-zero width serves a cached, downscaled
// JPEG variant; width zero serves the original for the lightbox.
func (c *ArtCache) Serve(w http.ResponseWriter, r *http.Request, kind string, id string, source string, width int) {
	baseKey := kind + "-" + id

	if width == 0 {
		path, err := c.ensure(r.Context(), baseKey, source)
		if err != nil {
			c.handleDownloadError(w, r, baseKey, err)
			return
		}
		c.serveFile(w, r, path)
		return
	}

	key := baseKey + "-w" + strconv.Itoa(width)
	if path, ok := c.find(key); ok {
		c.serveFile(w, r, path)
		return
	}
	if c.recentlyFailed(key) {
		http.Error(w, "art currently unavailable", http.StatusBadGateway)
		return
	}

	original, err := c.ensure(r.Context(), baseKey, source)
	if err != nil {
		c.handleDownloadError(w, r, baseKey, err)
		return
	}

	unlock := c.lock(key)
	defer unlock()

	// Another request may have built the variant while we waited.
	if path, ok := c.find(key); ok {
		c.serveFile(w, r, path)
		return
	}

	path, err := c.derive(key, width, original)
	if err != nil {
		// Unsupported formats (for example WebP) still work at full size.
		log.Printf("art %s: %v", key, err)
		c.serveFile(w, r, original)
		return
	}
	c.serveFile(w, r, path)
}

var errRecentlyFailed = errors.New("art recently failed")

func (c *ArtCache) handleDownloadError(w http.ResponseWriter, r *http.Request, key string, err error) {
	// A cancelled request means the browser navigated away; do not poison the
	// cache with a failure that would outlive the request.
	if r.Context().Err() != nil {
		return
	}
	if !errors.Is(err, errRecentlyFailed) {
		log.Printf("art %s: %v", key, err)
		c.markFailed(key)
	}
	http.Error(w, "art unavailable", http.StatusBadGateway)
}

// ensure returns the original cached file, downloading it when necessary.
func (c *ArtCache) ensure(ctx context.Context, key, source string) (string, error) {
	if path, ok := c.find(key); ok {
		return path, nil
	}
	if c.recentlyFailed(key) {
		return "", errRecentlyFailed
	}

	unlock := c.lock(key)
	defer unlock()

	// Another request may have finished the download while we waited.
	if path, ok := c.find(key); ok {
		return path, nil
	}
	return c.download(ctx, key, source)
}

// derive builds and caches a downscaled JPEG for an already cached original.
func (c *ArtCache) derive(key string, width int, source string) (string, error) {
	f, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return "", fmt.Errorf("decode %s: %w", filepath.Base(source), err)
	}

	data, err := encodeDownscaled(img, width)
	if err != nil {
		return "", err
	}

	tmp, err := os.CreateTemp(c.dir, ".tmp-"+key+"-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return "", err
	}

	final := filepath.Join(c.dir, key+".jpg")
	if err := os.Rename(tmpName, final); err != nil {
		return "", err
	}
	tmpName = ""

	c.clearFailure(key)
	return final, nil
}

// find returns the first cached file for a game ID.
func (c *ArtCache) find(key string) (string, bool) {
	for _, ext := range knownExts {
		path := filepath.Join(c.dir, key+ext)
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() {
			return path, true
		}
	}
	return "", false
}

func (c *ArtCache) download(ctx context.Context, key, source string) (string, error) {
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "nesdb/2.0 (+IGDB catalog)")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: status %s", source, resp.Status)
	}

	body := bufio.NewReader(io.LimitReader(resp.Body, maxArtBytes+1))
	head, _ := body.Peek(512)
	ext, ok := imageExt(http.DetectContentType(head))
	if !ok {
		return "", fmt.Errorf("download %s: unexpected content %q", source, http.DetectContentType(head))
	}

	tmp, err := os.CreateTemp(c.dir, ".tmp-"+key+"-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()

	n, err := io.Copy(tmp, io.LimitReader(body, maxArtBytes+1))
	if err != nil {
		_ = tmp.Close()
		return "", err
	}
	if n > maxArtBytes {
		_ = tmp.Close()
		return "", fmt.Errorf("image exceeds %d bytes", maxArtBytes)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return "", err
	}

	final := filepath.Join(c.dir, key+ext)
	if err := os.Rename(tmpName, final); err != nil {
		return "", err
	}
	tmpName = ""

	c.clearFailure(key)
	return final, nil
}

func (c *ArtCache) serveFile(w http.ResponseWriter, r *http.Request, path string) {
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "art unavailable", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		http.Error(w, "art unavailable", http.StatusInternalServerError)
		return
	}

	if ct := mime.TypeByExtension(filepath.Ext(path)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", fmt.Sprintf(`"%s-%d-%d"`, filepath.Base(path), info.Size(), info.ModTime().Unix()))
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), f)
}

func (c *ArtCache) lock(key string) func() {
	c.mu.Lock()
	mu, ok := c.locks[key]
	if !ok {
		mu = &sync.Mutex{}
		c.locks[key] = mu
	}
	c.mu.Unlock()

	mu.Lock()
	return mu.Unlock
}

func (c *ArtCache) recentlyFailed(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	at, ok := c.failed[key]
	if !ok {
		return false
	}
	if time.Since(at) > failureTTL {
		delete(c.failed, key)
		return false
	}
	return true
}

func (c *ArtCache) markFailed(key string) {
	c.mu.Lock()
	c.failed[key] = time.Now()
	c.mu.Unlock()
}

func (c *ArtCache) clearFailure(key string) {
	c.mu.Lock()
	delete(c.failed, key)
	c.mu.Unlock()
}

func imageExt(contentType string) (string, bool) {
	switch strings.TrimSpace(strings.Split(contentType, ";")[0]) {
	case "image/jpeg":
		return ".jpg", true
	case "image/png":
		return ".png", true
	case "image/webp":
		return ".webp", true
	case "image/gif":
		return ".gif", true
	default:
		return "", false
	}
}
