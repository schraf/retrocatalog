package main

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const launchBoxMetadataURL = "https://gamesdb.launchbox-app.com/Metadata.zip"

// minRatingVotes is the point at which a source's score is trusted over the
// other source. LaunchBox is primary, but a one-vote LaunchBox entry should not
// replace an established IGDB score.
const minRatingVotes = 5

// launchBoxGame is the subset of a LaunchBox metadata record needed for
// ratings. The bulk export contains many more fields; they are decoded and
// discarded.
type launchBoxGame struct {
	DatabaseID  int
	Name        string
	Platform    string
	ReleaseYear int
	ReleaseType string
	Rating      float64
	RatingCount int
}

// launchBoxClient downloads and parses the LaunchBox Games Database bulk
// metadata export. A local Metadata.xml or Metadata.zip can be supplied instead
// of downloading, which keeps refreshes reproducible.
type launchBoxClient struct {
	httpClient  *http.Client
	metadataURL string
	localPath   string
	tempDir     string
}

func newLaunchBoxClient(localPath string) *launchBoxClient {
	return &launchBoxClient{
		httpClient:  &http.Client{Timeout: 10 * time.Minute},
		metadataURL: launchBoxMetadataURL,
		localPath:   localPath,
	}
}

func (c *launchBoxClient) fetch(ctx context.Context) ([]launchBoxGame, error) {
	path := c.localPath
	cleanup := func() {}
	if path == "" {
		var err error
		path, err = c.download(ctx)
		if err != nil {
			return nil, err
		}
		cleanup = func() { _ = os.Remove(path) }
	}
	defer cleanup()

	games, stats, err := parseLaunchBoxFile(path)
	if err != nil {
		return nil, err
	}
	log.Printf("launchbox: parsed %d records; kept %d released entries; skipped %d other release types and %d other platforms; %d kept without a year",
		stats.Total, stats.Kept, stats.SkippedType, stats.SkippedPlatform, stats.NoYear)
	return games, nil
}

func (c *launchBoxClient) download(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.metadataURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "retrocatalog/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download LaunchBox metadata: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download LaunchBox metadata: status %s", resp.Status)
	}

	file, err := os.CreateTemp(c.tempDir, "launchbox-metadata-*.zip")
	if err != nil {
		return "", err
	}
	name := file.Name()
	if _, err := io.Copy(file, resp.Body); err != nil {
		file.Close()
		_ = os.Remove(name)
		return "", fmt.Errorf("download LaunchBox metadata: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

type launchBoxParseStats struct {
	Total           int
	Kept            int
	SkippedType     int
	SkippedPlatform int
	NoYear          int
}

type launchBoxRecord struct {
	Name                 string  `xml:"Name"`
	ReleaseYear          int     `xml:"ReleaseYear"`
	ReleaseDate          string  `xml:"ReleaseDate"`
	ReleaseType          string  `xml:"ReleaseType"`
	DatabaseID           int     `xml:"DatabaseID"`
	CommunityRating      float64 `xml:"CommunityRating"`
	CommunityRatingCount int     `xml:"CommunityRatingCount"`
	Platform             string  `xml:"Platform"`
}

func parseLaunchBoxFile(path string) ([]launchBoxGame, launchBoxParseStats, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, launchBoxParseStats{}, err
	}
	defer file.Close()

	if !strings.EqualFold(filepath.Ext(path), ".zip") {
		return parseLaunchBoxXML(file)
	}

	info, err := file.Stat()
	if err != nil {
		return nil, launchBoxParseStats{}, err
	}
	reader, err := zip.NewReader(file, info.Size())
	if err != nil {
		return nil, launchBoxParseStats{}, fmt.Errorf("open LaunchBox archive: %w", err)
	}
	for _, entry := range reader.File {
		if filepath.Base(entry.Name) != "Metadata.xml" {
			continue
		}
		body, err := entry.Open()
		if err != nil {
			return nil, launchBoxParseStats{}, fmt.Errorf("open LaunchBox Metadata.xml: %w", err)
		}
		defer body.Close()
		return parseLaunchBoxXML(body)
	}
	return nil, launchBoxParseStats{}, errors.New("LaunchBox archive does not contain Metadata.xml")
}

func parseLaunchBoxXML(r io.Reader) ([]launchBoxGame, launchBoxParseStats, error) {
	decoder := xml.NewDecoder(r)
	// The overview text contains HTML entities such as &nbsp; and &mdash;.
	decoder.Strict = false
	decoder.Entity = xml.HTMLEntity

	var stats launchBoxParseStats
	var games []launchBoxGame
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, stats, fmt.Errorf("parse LaunchBox metadata: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "Game" {
			continue
		}

		var record launchBoxRecord
		if err := decoder.DecodeElement(&record, &start); err != nil {
			return nil, stats, fmt.Errorf("parse LaunchBox game: %w", err)
		}
		stats.Total++

		platform := launchBoxPlatformSlug(record.Platform)
		if platform == "" {
			stats.SkippedPlatform++
			continue
		}
		releaseType := strings.TrimSpace(record.ReleaseType)
		switch releaseType {
		case "", "Released":
		default:
			// Unlicensed, Homebrew, ROM Hack, and Unreleased entries are not
			// retail releases.
			stats.SkippedType++
			continue
		}

		year := record.ReleaseYear
		if year <= 0 && len(record.ReleaseDate) >= 4 {
			if parsed, err := strconv.Atoi(record.ReleaseDate[:4]); err == nil {
				year = parsed
			}
		}
		if year <= 0 {
			stats.NoYear++
		}

		rating := record.CommunityRating
		if rating < 0 {
			rating = 0
		}
		if rating > 5 {
			rating = 5
		}
		count := record.CommunityRatingCount
		if count < 0 {
			count = 0
		}

		games = append(games, launchBoxGame{
			DatabaseID:  record.DatabaseID,
			Name:        strings.TrimSpace(record.Name),
			Platform:    platform,
			ReleaseYear: year,
			ReleaseType: releaseType,
			Rating:      rating,
			RatingCount: count,
		})
		stats.Kept++
	}
	return games, stats, nil
}

func launchBoxPlatformSlug(name string) string {
	name = strings.TrimSpace(name)
	for _, platform := range supportedPlatforms {
		if platform.LaunchBoxName == name {
			return platform.Slug
		}
	}
	return ""
}

type launchBoxCandidate struct {
	game    *launchBoxGame
	tokens  map[string]struct{}
	numbers map[string]struct{}
}

type launchBoxMatcher struct {
	byKey map[string][]*launchBoxCandidate
	all   []*launchBoxCandidate
}

func newLaunchBoxMatcher(platform string, games []launchBoxGame) *launchBoxMatcher {
	matcher := &launchBoxMatcher{byKey: make(map[string][]*launchBoxCandidate)}
	for i := range games {
		game := &games[i]
		if game.Platform != platform || game.Name == "" {
			continue
		}
		key := matchKey(game.Name)
		tokens := keyTokenSet(key)
		candidate := &launchBoxCandidate{game: game, tokens: tokens, numbers: keyNumberSet(tokens)}
		matcher.byKey[key] = append(matcher.byKey[key], candidate)
		matcher.all = append(matcher.all, candidate)
	}
	return matcher
}

type launchBoxMatchKind int

const (
	matchNone launchBoxMatchKind = iota
	matchExact
	matchAlias
	matchFuzzy
)

// launchBoxAliases cover titles that do not share enough tokens to match
// automatically. The SNES Ninja Gaiden ports are rated as the trilogy release,
// and the Game Boy Advance Super Mario ports and Game Boy Color compilations
// carry ratings under their compilation titles.
var launchBoxAliases = []struct {
	Platform string
	Title    string
	Target   string
}{
	{"snes", "Ninja Gaiden", "Ninja Gaiden Trilogy"},
	{"snes", "Ninja Gaiden II: The Dark Sword of Chaos", "Ninja Gaiden Trilogy"},
	{"snes", "Ninja Gaiden III: The Ancient Ship of Doom", "Ninja Gaiden Trilogy"},
	{"gba", "Super Mario Bros. 3", "Super Mario Advance 4: Super Mario Bros. 3"},
	{"gba", "Super Mario World", "Super Mario Advance 2: Super Mario World"},
	{"gba", "Yoshi's Island", "Super Mario Advance 3: Yoshi's Island"},
	{"gbc", "Dragon Warrior I", "Dragon Warrior I & II"},
	{"gbc", "Dragon Warrior II", "Dragon Warrior I & II"},
	{"gbc", "Déjà Vu I", "Déjà Vu I & II: The Casebooks of Ace Harding"},
	{"gbc", "Déjà Vu II", "Déjà Vu I & II: The Casebooks of Ace Harding"},
	{"gb", "Alien Olympics 2044 AD", "Alien Olympics"},
}

var launchBoxAliasIndex = func() map[string]string {
	index := make(map[string]string)
	for _, alias := range launchBoxAliases {
		index[alias.Platform+"\x00"+matchKey(alias.Title)] = matchKey(alias.Target)
	}
	return index
}()

func launchBoxAlias(platform, key string) (string, bool) {
	target, ok := launchBoxAliasIndex[platform+"\x00"+key]
	return target, ok
}

// launchBoxNoMatch lists titles whose closest LaunchBox entry is a different
// game (usually a sequel whose subtitle is missing from the LaunchBox title).
// They stay unrated rather than inheriting another game's score.
var launchBoxNoMatch = []struct {
	Platform string
	Title    string
}{
	{"gb", "Die Maus: Verrückte Olympiade"},
	{"gb", "The Blues Brothers: Jukebox Adventure"},
	{"gba", "DragonBasic Space Invaders"},
	{"gba", "Medabots AX: Metabee Version"},
	{"gba", "Medabots AX: Rokusho Version"},
	{"gbc", "Tom and Jerry in House Trap"},
	{"snes", "Jack Nicklaus' Unlimited Golf & Course Design"},
	{"snes", "Uncharted Waters: New Horizons"},
}

var launchBoxNoMatchIndex = func() map[string]struct{} {
	index := make(map[string]struct{}, len(launchBoxNoMatch))
	for _, entry := range launchBoxNoMatch {
		index[entry.Platform+"\x00"+matchKey(entry.Title)] = struct{}{}
	}
	return index
}()

func launchBoxBlocked(platform, key string) bool {
	_, ok := launchBoxNoMatchIndex[platform+"\x00"+key]
	return ok
}

func (m *launchBoxMatcher) match(game Game) (*launchBoxCandidate, launchBoxMatchKind) {
	key := matchKey(game.Name)
	if candidates, ok := m.byKey[key]; ok {
		// An exact title is a strong signal. If all exact entries are unrated,
		// do not fall through to a fuzzy match against a different game.
		return m.best(candidates, game.ReleaseYear), matchExact
	}
	if target, ok := launchBoxAlias(game.Platform, key); ok {
		if candidates, ok := m.byKey[target]; ok {
			if candidate := m.best(candidates, game.ReleaseYear); candidate != nil &&
				yearCompatible(game.ReleaseYear, candidate.game.ReleaseYear, 2) {
				return candidate, matchAlias
			}
		}
	}
	if launchBoxBlocked(game.Platform, key) {
		return nil, matchNone
	}
	return m.fuzzy(game), matchFuzzy
}

func (m *launchBoxMatcher) best(candidates []*launchBoxCandidate, year int) *launchBoxCandidate {
	var best *launchBoxCandidate
	bestDistance := 0
	bestCount := -1
	for _, candidate := range candidates {
		if candidate.game.Rating <= 0 || candidate.game.RatingCount <= 0 {
			continue
		}
		distance := yearDistance(year, candidate.game.ReleaseYear)
		if best == nil || distance < bestDistance || (distance == bestDistance && candidate.game.RatingCount > bestCount) {
			best = candidate
			bestDistance = distance
			bestCount = candidate.game.RatingCount
		}
	}
	return best
}

func (m *launchBoxMatcher) fuzzy(game Game) *launchBoxCandidate {
	want := keyTokenSet(matchKey(game.Name))
	if len(want) == 0 {
		return nil
	}
	wantNumbers := keyNumberSet(want)

	const jaccardThreshold = 0.72
	const containmentRatio = 0.4

	var best *launchBoxCandidate
	bestScore := 0.0
	for _, candidate := range m.all {
		if candidate.game.Rating <= 0 || candidate.game.RatingCount <= 0 {
			continue
		}
		if !yearCompatible(game.ReleaseYear, candidate.game.ReleaseYear, 2) {
			continue
		}
		// A shared word is not enough when the numbers differ: Pac-Man 2 is not
		// Pac-Man, and Madden '92 is not Madden '91.
		if !sameNumberSet(wantNumbers, candidate.numbers) {
			continue
		}

		score := nameSimilarity(want, candidate.tokens)
		short, long := len(candidate.tokens), len(want)
		if short > long {
			short, long = long, short
		}
		contained := tokenSubset(candidate.tokens, want) || tokenSubset(want, candidate.tokens)
		if contained && short >= 2 && float64(short)/float64(long) >= containmentRatio &&
			yearCompatible(game.ReleaseYear, candidate.game.ReleaseYear, 1) {
			// A dropped subtitle ("Doom II" for "Doom II: Hell on Earth") is a
			// strong match, but keeping more of the long title makes it
			// stronger.
			score = 0.7 + 0.3*score
		}
		if score < jaccardThreshold {
			continue
		}
		if best == nil || score > bestScore || (score == bestScore && candidate.game.RatingCount > best.game.RatingCount) {
			best = candidate
			bestScore = score
		}
	}
	return best
}

func yearCompatible(a, b, max int) bool {
	if a == 0 || b == 0 {
		return true
	}
	return yearDistance(a, b) <= max
}

func yearDistance(a, b int) int {
	if a == 0 || b == 0 {
		return 10
	}
	distance := a - b
	if distance < 0 {
		distance = -distance
	}
	return distance
}

type launchBoxMergeStats struct {
	Games     int
	Matched   int
	Exact     int
	Alias     int
	Fuzzy     int
	LaunchBox int
	IGDB      int
	Unrated   int
}

// mergeLaunchBoxRatings attaches LaunchBox ratings and selects the source used
// by the catalog. Both raw scores stay on the record for the detail panel.
func mergeLaunchBoxRatings(games []Game, entries []launchBoxGame) launchBoxMergeStats {
	matchers := make(map[string]*launchBoxMatcher, len(supportedPlatforms))
	for _, platform := range supportedPlatforms {
		matchers[platform.Slug] = newLaunchBoxMatcher(platform.Slug, entries)
	}

	var stats launchBoxMergeStats
	stats.Games = len(games)
	for i := range games {
		game := &games[i]
		if game.RatingSource == "" && game.Rating > 0 && game.IGDBRatingCount == 0 {
			game.IGDBRating = game.Rating
			game.IGDBRatingCount = game.RatingCount
		}

		if matcher := matchers[game.Platform]; matcher != nil {
			candidate, kind := matcher.match(*game)
			if candidate != nil {
				game.LaunchBoxID = candidate.game.DatabaseID
				game.LaunchBoxRating = candidate.game.Rating
				game.LaunchBoxRatingCount = candidate.game.RatingCount
				stats.Matched++
				switch kind {
				case matchExact:
					stats.Exact++
				case matchAlias:
					stats.Alias++
				case matchFuzzy:
					stats.Fuzzy++
				}
			}
		}

		switch {
		case game.LaunchBoxRating > 0 && game.LaunchBoxRatingCount >= minRatingVotes:
			game.Rating = game.LaunchBoxRating
			game.RatingCount = game.LaunchBoxRatingCount
			game.RatingSource = "launchbox"
			stats.LaunchBox++
		case game.IGDBRating > 0 && game.IGDBRatingCount >= minRatingVotes:
			game.Rating = game.IGDBRating
			game.RatingCount = game.IGDBRatingCount
			game.RatingSource = "igdb"
			stats.IGDB++
		case game.LaunchBoxRating > 0 && game.LaunchBoxRatingCount > 0:
			game.Rating = game.LaunchBoxRating
			game.RatingCount = game.LaunchBoxRatingCount
			game.RatingSource = "launchbox"
			stats.LaunchBox++
		case game.IGDBRating > 0 && game.IGDBRatingCount > 0:
			game.Rating = game.IGDBRating
			game.RatingCount = game.IGDBRatingCount
			game.RatingSource = "igdb"
			stats.IGDB++
		default:
			game.Rating = 0
			game.RatingCount = 0
			game.RatingSource = ""
			stats.Unrated++
		}
		game.RatingScale = 5
	}
	return stats
}
