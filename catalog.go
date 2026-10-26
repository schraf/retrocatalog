package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
)

// allowedArtHosts limits the art proxy to the hosts that appear in the catalog.
var allowedArtHosts = map[string]bool{
	"images.launchbox-app.com": true,
	"thumbnails.libretro.com":  true,
}

// Game mirrors one record in games.json.
type Game struct {
	Name        string   `json:"name"`
	Publisher   string   `json:"publisher"`
	Developer   string   `json:"developer"`
	Players     int      `json:"players"`
	ReleaseYear int      `json:"release_year"`
	Genre       string   `json:"genre"`
	Tags        []string `json:"tags"`
	Rating      float64  `json:"rating"`
	RatingCount int      `json:"rating_count"`
	RatingScale int      `json:"rating_scale"`
	MaxPlayers  int      `json:"max_players"`
	Cooperative bool     `json:"cooperative"`
	ESRB        string   `json:"esrb"`
	Overview    string   `json:"overview"`
	LaunchboxID int      `json:"launchbox_id"`
	BoxArt      string   `json:"box_art"`
	Screenshot  string   `json:"screenshot"`
}

// priorWeight controls how many ratings a game needs before its own score
// outweighs the catalog average in the weighted rating.
const priorWeight = 75.0

// GenreCount is a facet used by the filter chips.
type GenreCount struct {
	Genre string `json:"genre"`
	Count int    `json:"count"`
}

// IndexGame is the compact shape sent to the shelf grid.
type IndexGame struct {
	ID             int     `json:"id"`
	Name           string  `json:"name"`
	Year           int     `json:"year"`
	Genre          string  `json:"genre"`
	Rating         float64 `json:"rating"`
	RatingCount    int     `json:"rating_count"`
	WeightedRating float64 `json:"weighted_rating"`
	Publisher      string  `json:"publisher"`
	Developer      string  `json:"developer"`
	Players        int     `json:"players"`
	MaxPlayers     int     `json:"max_players"`
	Cooperative    bool    `json:"cooperative"`
	ESRB           string  `json:"esrb"`
}

// Catalog is the immutable in-memory view of games.json.
type Catalog struct {
	games      []*Game
	byID       map[int]*Game
	index      []IndexGame
	genres     []GenreCount
	yearMin    int
	yearMax    int
	ratingMin  float64
	ratingMax  float64
	ratingMean float64
}

// LoadCatalog reads and validates the single catalog file.
func LoadCatalog(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read catalog: %w", err)
	}

	var games []Game
	if err := json.Unmarshal(data, &games); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(games) == 0 {
		return nil, fmt.Errorf("%s contains no games", path)
	}

	c := &Catalog{
		games: make([]*Game, 0, len(games)),
		byID:  make(map[int]*Game, len(games)),
	}
	genreCounts := make(map[string]int)
	var voteSum, voteCount float64
	first := true

	for i := range games {
		g := &games[i]
		if g.Name == "" {
			return nil, fmt.Errorf("record %d has no name", i)
		}
		if g.LaunchboxID <= 0 {
			return nil, fmt.Errorf("%q has invalid launchbox_id %d", g.Name, g.LaunchboxID)
		}
		if _, dup := c.byID[g.LaunchboxID]; dup {
			return nil, fmt.Errorf("duplicate launchbox_id %d (%q)", g.LaunchboxID, g.Name)
		}
		if g.Genre == "" {
			return nil, fmt.Errorf("%q has no genre", g.Name)
		}
		if g.RatingScale <= 0 {
			g.RatingScale = 5
		}
		if err := validateArtURL(g.BoxArt); err != nil {
			return nil, fmt.Errorf("%q box art: %w", g.Name, err)
		}
		if err := validateArtURL(g.Screenshot); err != nil {
			return nil, fmt.Errorf("%q screenshot: %w", g.Name, err)
		}

		c.byID[g.LaunchboxID] = g
		c.games = append(c.games, g)
		genreCounts[g.Genre]++
		voteSum += g.Rating * float64(g.RatingCount)
		voteCount += float64(g.RatingCount)
		c.index = append(c.index, IndexGame{
			ID:          g.LaunchboxID,
			Name:        g.Name,
			Year:        g.ReleaseYear,
			Genre:       g.Genre,
			Rating:      g.Rating,
			RatingCount: g.RatingCount,
			Publisher:   g.Publisher,
			Developer:   g.Developer,
			Players:     g.Players,
			MaxPlayers:  g.MaxPlayers,
			Cooperative: g.Cooperative,
			ESRB:        g.ESRB,
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

	return c, nil
}

// weightedRating shrinks scores with few votes toward the catalog average.
func weightedRating(rating float64, count int, mean float64) float64 {
	votes := float64(count)
	return (votes*rating + priorWeight*mean) / (votes + priorWeight)
}

func validateArtURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "https" {
		return fmt.Errorf("must use https, got %q", raw)
	}
	if !allowedArtHosts[u.Hostname()] {
		return fmt.Errorf("host %q is not allowed", u.Hostname())
	}
	if u.Path == "" || u.Path == "/" {
		return fmt.Errorf("has no path: %q", raw)
	}
	return nil
}

// ByID returns the full record for a game, or nil.
func (c *Catalog) ByID(id int) *Game {
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

// Total returns the number of games in the catalog.
func (c *Catalog) Total() int {
	return len(c.games)
}
