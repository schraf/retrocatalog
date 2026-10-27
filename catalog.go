package main

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// allowedArtHosts limits the art proxy to the hosts that appear in the catalog.
var allowedArtHosts = map[string]bool{
	"images.igdb.com": true,
}

// Game mirrors one record in the catalog cache.
type Game struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Platform    string   `json:"platform"`
	Publisher   string   `json:"publisher"`
	Developer   string   `json:"developer"`
	ReleaseYear int      `json:"release_year"`
	Genres      []string `json:"genres"`
	Rating      float64  `json:"rating"`
	RatingCount int      `json:"rating_count"`
	RatingScale int      `json:"rating_scale"`
	MaxPlayers  int      `json:"max_players"`
	Cooperative bool     `json:"cooperative"`
	ESRB        string   `json:"esrb"`
	Overview    string   `json:"overview"`
	BoxArt      string   `json:"box_art"`
	Screenshots []string `json:"screenshots"`
}

// priorWeight controls how many ratings a game needs before its own score
// outweighs the catalog average in the weighted rating.
const priorWeight = 75.0

// maxScreenshots is the number of IGDB screenshots kept per game.
const maxScreenshots = 3

// GenreCount is a facet used by the filter chips.
type GenreCount struct {
	Genre string `json:"genre"`
	Count int    `json:"count"`
}

// IndexGame is the compact shape sent to the shelf grid.
type IndexGame struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Year           int      `json:"year"`
	Genres         []string `json:"genres"`
	Platform       string   `json:"platform"`
	Rating         float64  `json:"rating"`
	RatingCount    int      `json:"rating_count"`
	WeightedRating float64  `json:"weighted_rating"`
	Publisher      string   `json:"publisher"`
	Developer      string   `json:"developer"`
	MaxPlayers     int      `json:"max_players"`
	Cooperative    bool     `json:"cooperative"`
	ESRB           string   `json:"esrb"`
	BoxArt         string   `json:"box_art"`
}

// Catalog is the immutable in-memory view of the catalog cache.
type Catalog struct {
	games      []*Game
	byID       map[string]*Game
	index      []IndexGame
	genres     []GenreCount
	platforms  []PlatformCount
	yearMin    int
	yearMax    int
	ratingMin  float64
	ratingMax  float64
	ratingMean float64
}

// buildCatalog validates normalized records and derives the indexes and facets.
func buildCatalog(games []Game) (*Catalog, error) {
	if len(games) == 0 {
		return nil, fmt.Errorf("catalog contains no games")
	}

	c := &Catalog{
		games: make([]*Game, 0, len(games)),
		byID:  make(map[string]*Game, len(games)),
	}
	genreCounts := make(map[string]int)
	platformCounts := make(map[string]int, len(supportedPlatforms))
	for _, p := range supportedPlatforms {
		platformCounts[p.Slug] = 0
	}
	var voteSum, voteCount float64
	first := true

	for i := range games {
		g := &games[i]
		if g.Name == "" {
			return nil, fmt.Errorf("record %d has no name", i)
		}
		_, slug, ok := parseGameID(g.ID)
		if !ok {
			return nil, fmt.Errorf("%q has invalid id %q", g.Name, g.ID)
		}
		if slug != g.Platform {
			return nil, fmt.Errorf("%q id platform %q does not match platform %q", g.Name, slug, g.Platform)
		}
		if g.ReleaseYear <= 0 {
			return nil, fmt.Errorf("%q has invalid release_year %d", g.Name, g.ReleaseYear)
		}
		if _, dup := c.byID[g.ID]; dup {
			return nil, fmt.Errorf("duplicate id %q (%q)", g.ID, g.Name)
		}
		if platform := platformBySlug(g.Platform); g.ReleaseYear > platform.LastYear {
			// Treat anything after the commercial lifespan as homebrew, a
			// bootleg, or a modern re-release and leave it out.
			continue
		}

		g.RatingScale = 5
		if g.Rating < 0 {
			g.Rating = 0
		}
		if g.Rating > 5 {
			g.Rating = 5
		}
		if g.RatingCount < 0 {
			g.RatingCount = 0
		}
		if g.MaxPlayers < 0 {
			g.MaxPlayers = 0
		}
		g.Genres = normalizeGenres(g.Genres)

		if err := validateArtURL(g.BoxArt); err != nil {
			return nil, fmt.Errorf("%q box art: %w", g.Name, err)
		}
		screenshots, err := normalizeScreenshots(g.Screenshots)
		if err != nil {
			return nil, fmt.Errorf("%q screenshot: %w", g.Name, err)
		}
		g.Screenshots = screenshots

		if g.BoxArt == "" {
			// The grid depends on cover art; skip entries that lack it.
			continue
		}

		c.byID[g.ID] = g
		c.games = append(c.games, g)
		for _, genre := range g.Genres {
			genreCounts[genre]++
		}
		platformCounts[g.Platform]++
		voteSum += g.Rating * float64(g.RatingCount)
		voteCount += float64(g.RatingCount)

		c.index = append(c.index, IndexGame{
			ID:          g.ID,
			Name:        g.Name,
			Year:        g.ReleaseYear,
			Genres:      g.Genres,
			Platform:    g.Platform,
			Rating:      g.Rating,
			RatingCount: g.RatingCount,
			Publisher:   g.Publisher,
			Developer:   g.Developer,
			MaxPlayers:  g.MaxPlayers,
			Cooperative: g.Cooperative,
			ESRB:        g.ESRB,
			BoxArt:      g.BoxArt,
		})

		if first {
			c.yearMin, c.yearMax = g.ReleaseYear, g.ReleaseYear
			c.ratingMin, c.ratingMax = g.Rating, g.Rating
			first = false
		} else {
			if g.ReleaseYear < c.yearMin {
				c.yearMin = g.ReleaseYear
			}
			if g.ReleaseYear > c.yearMax {
				c.yearMax = g.ReleaseYear
			}
			if g.Rating < c.ratingMin {
				c.ratingMin = g.Rating
			}
			if g.Rating > c.ratingMax {
				c.ratingMax = g.Rating
			}
		}
	}

	if voteCount > 0 {
		c.ratingMean = voteSum / voteCount
	}
	for i := range c.index {
		entry := &c.index[i]
		entry.WeightedRating = weightedRating(entry.Rating, entry.RatingCount, c.ratingMean)
	}

	for genre, count := range genreCounts {
		c.genres = append(c.genres, GenreCount{Genre: genre, Count: count})
	}
	sort.Slice(c.genres, func(i, j int) bool {
		if c.genres[i].Count != c.genres[j].Count {
			return c.genres[i].Count > c.genres[j].Count
		}
		return c.genres[i].Genre < c.genres[j].Genre
	})

	c.platforms = make([]PlatformCount, 0, len(supportedPlatforms))
	for _, p := range supportedPlatforms {
		c.platforms = append(c.platforms, PlatformCount{
			Slug:  p.Slug,
			Name:  p.Name,
			Short: p.Short,
			Count: platformCounts[p.Slug],
		})
	}

	return c, nil
}

func normalizeGenres(genres []string) []string {
	out := make([]string, 0, len(genres))
	seen := make(map[string]bool, len(genres))
	for _, genre := range genres {
		genre = strings.TrimSpace(genre)
		if genre == "" {
			continue
		}
		key := strings.ToLower(genre)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, genre)
	}
	return out
}

// normalizeScreenshots keeps at most three valid, unique IGDB screenshot URLs.
func normalizeScreenshots(urls []string) ([]string, error) {
	out := make([]string, 0, len(urls))
	seen := make(map[string]bool, len(urls))
	for _, raw := range urls {
		raw = strings.TrimSpace(raw)
		if raw == "" || seen[raw] {
			continue
		}
		if err := validateArtURL(raw); err != nil {
			return nil, err
		}
		seen[raw] = true
		out = append(out, raw)
		if len(out) == maxScreenshots {
			break
		}
	}
	return out, nil
}

// weightedRating shrinks scores with few votes toward the catalog average.
func weightedRating(rating float64, count int, mean float64) float64 {
	votes := float64(count)
	return (votes*rating + priorWeight*mean) / (votes + priorWeight)
}

func validateArtURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "https" {
		return fmt.Errorf("must use https, got %q", raw)
	}
	if !allowedArtHosts[strings.ToLower(u.Hostname())] {
		return fmt.Errorf("host %q is not allowed", u.Hostname())
	}
	if u.Path == "" || u.Path == "/" {
		return fmt.Errorf("has no path: %q", raw)
	}
	return nil
}

// ByID returns the full record for a game, or nil.
func (c *Catalog) ByID(id string) *Game {
	return c.byID[id]
}

// Index returns the compact records used by the grid.
func (c *Catalog) Index() []IndexGame {
	return c.index
}

// Genres returns genre facets, largest first.
func (c *Catalog) Genres() []GenreCount {
	return c.genres
}

// Platforms returns platform facets in registry order.
func (c *Catalog) Platforms() []PlatformCount {
	return c.platforms
}

// Total returns the number of games in the catalog.
func (c *Catalog) Total() int {
	return len(c.games)
}
