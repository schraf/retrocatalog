package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	igdbAPIBaseURL = "https://api.igdb.com/v4"
	igdbTokenURL   = "https://id.twitch.tv/oauth2/token"

	igdbPageSize    = 500
	igdbRequestGap  = 250 * time.Millisecond
	igdbMaxAttempts = 4

	catalogCacheName = "catalog-v2.json"
	catalogCacheTTL  = 24 * time.Hour

	igdbGameTypeMain = 0
	igdbGameTypePort = 11
	// IGDB rarely populates game_status for retro games, so the query accepts
	// both "Released" and null. Other statuses remain excluded.
	igdbGameStatusReleased = 0
	igdbRegionNorthAmerica = 2
	igdbRegionWorldwide    = 8
)

const igdbGameFields = "id,name,game_type," +
	"release_dates.platform,release_dates.y,release_dates.date,release_dates.release_region,release_dates.status.name," +
	"genres.name," +
	"involved_companies.company.name,involved_companies.developer,involved_companies.publisher," +
	"multiplayer_modes.platform,multiplayer_modes.offlinemax,multiplayer_modes.onlinemax," +
	"multiplayer_modes.offlinecoopmax,multiplayer_modes.onlinecoopmax," +
	"multiplayer_modes.campaigncoop,multiplayer_modes.offlinecoop,multiplayer_modes.onlinecoop," +
	"age_ratings.organization.name,age_ratings.rating_category.rating," +
	"rating,rating_count,total_rating,total_rating_count,summary,cover.image_id,screenshots.image_id"

// fetchFunc fetches a normalized catalog, usually from IGDB.
type fetchFunc func(context.Context) ([]Game, error)

// igdbClient is a minimal IGDB v4 client. Base URLs, pacing, and page size are
// fields so tests can run without network access or real sleeps.
type igdbClient struct {
	clientID     string
	clientSecret string
	httpClient   *http.Client
	apiBase      string
	tokenURL     string
	pageSize     int
	requestGap   time.Duration
	sleep        func(time.Duration)
	token        string
}

func newIGDBClient(clientID, clientSecret string) *igdbClient {
	return &igdbClient{
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   &http.Client{Timeout: 60 * time.Second},
		apiBase:      igdbAPIBaseURL,
		tokenURL:     igdbTokenURL,
		pageSize:     igdbPageSize,
		requestGap:   igdbRequestGap,
		sleep:        time.Sleep,
	}
}

func (c *igdbClient) authenticate(ctx context.Context) error {
	u, err := url.Parse(c.tokenURL)
	if err != nil {
		return fmt.Errorf("IGDB auth URL: %w", err)
	}
	q := u.Query()
	q.Set("client_id", c.clientID)
	q.Set("client_secret", c.clientSecret)
	q.Set("grant_type", "client_credentials")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("IGDB auth: status %s", resp.Status)
	}

	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("IGDB auth response: %w", err)
	}
	if payload.AccessToken == "" {
		return errors.New("IGDB auth: empty access token")
	}
	c.token = payload.AccessToken
	return nil
}

func (c *igdbClient) post(ctx context.Context, endpoint, body string, out any) error {
	reauthenticated := false
	for attempt := 1; attempt <= igdbMaxAttempts; attempt++ {
		if c.token == "" {
			if err := c.authenticate(ctx); err != nil {
				return err
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase+"/"+endpoint, strings.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Client-ID", c.clientID)
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Content-Type", "text/plain")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if attempt == igdbMaxAttempts {
				return err
			}
			c.sleep(igdbBackoff(attempt))
			continue
		}

		status := resp.StatusCode
		if status == http.StatusOK {
			err := json.NewDecoder(resp.Body).Decode(out)
			resp.Body.Close()
			return err
		}
		resp.Body.Close()

		switch {
		case status == http.StatusUnauthorized && !reauthenticated:
			reauthenticated = true
			c.token = ""
			continue
		case status == http.StatusTooManyRequests || status >= http.StatusInternalServerError:
			if attempt == igdbMaxAttempts {
				return fmt.Errorf("IGDB %s: status %s", endpoint, resp.Status)
			}
			c.sleep(igdbBackoff(attempt))
			continue
		default:
			return fmt.Errorf("IGDB %s: status %s", endpoint, resp.Status)
		}
	}
	return fmt.Errorf("IGDB %s: exhausted retries", endpoint)
}

func igdbBackoff(attempt int) time.Duration {
	d := time.Duration(attempt*attempt) * 250 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

func (c *igdbClient) fetchCatalog(ctx context.Context) ([]Game, error) {
	raw, err := c.fetchGames(ctx)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, errors.New("IGDB returned no games; the platform, game type, or game status filters may be too strict")
	}

	candidates, stats := normalizeGames(raw)
	games, merged := dedupeCandidates(candidates)
	if len(games) == 0 {
		return nil, errors.New("IGDB returned no US-eligible games")
	}
	log.Printf("igdb: fetched %d games (%d platform releases); kept %d; skipped %d without a US date, %d without a year, %d canceled dates; merged %d duplicates",
		len(raw), stats.platformReleases, len(games), stats.noUS, stats.noYear, stats.canceled, merged)
	return games, nil
}

func (c *igdbClient) fetchGames(ctx context.Context) ([]igdbGame, error) {
	base := fmt.Sprintf(
		"fields %s; where platforms = (%s) & game_type = (%d,%d) & "+
			"(game_status = %d | game_status = null); sort id asc; limit %d; offset ",
		igdbGameFields,
		strings.Join(supportedPlatformIDs(), ","),
		igdbGameTypeMain, igdbGameTypePort,
		igdbGameStatusReleased,
		c.pageSize,
	)

	var all []igdbGame
	offset := 0
	for {
		var page []igdbGame
		if err := c.post(ctx, "games", base+strconv.Itoa(offset)+";", &page); err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < c.pageSize {
			break
		}
		offset += len(page)
		if c.requestGap > 0 {
			c.sleep(c.requestGap)
		}
	}
	return all, nil
}

type igdbGame struct {
	ID                int                   `json:"id"`
	Name              string                `json:"name"`
	GameType          int                   `json:"game_type"`
	ReleaseDates      []igdbReleaseDate     `json:"release_dates"`
	Genres            []igdbGenre           `json:"genres"`
	InvolvedCompanies []igdbInvolvedCompany `json:"involved_companies"`
	MultiplayerModes  []igdbMultiplayerMode `json:"multiplayer_modes"`
	AgeRatings        []igdbAgeRating       `json:"age_ratings"`
	Rating            float64               `json:"rating"`
	RatingCount       int                   `json:"rating_count"`
	TotalRating       float64               `json:"total_rating"`
	TotalRatingCount  int                   `json:"total_rating_count"`
	Summary           string                `json:"summary"`
	Cover             *igdbImage            `json:"cover"`
	Screenshots       []igdbImage           `json:"screenshots"`
}

type igdbReleaseDate struct {
	Platform      int                `json:"platform"`
	Y             int                `json:"y"`
	Date          int64              `json:"date"`
	ReleaseRegion int                `json:"release_region"`
	Status        *igdbReleaseStatus `json:"status"`
}

type igdbReleaseStatus struct {
	Name string `json:"name"`
}

type igdbGenre struct {
	Name string `json:"name"`
}

type igdbInvolvedCompany struct {
	Company   igdbCompany `json:"company"`
	Developer bool        `json:"developer"`
	Publisher bool        `json:"publisher"`
}

type igdbCompany struct {
	Name string `json:"name"`
}

type igdbMultiplayerMode struct {
	Platform       int  `json:"platform"`
	OfflineMax     int  `json:"offlinemax"`
	OnlineMax      int  `json:"onlinemax"`
	OfflineCoopMax int  `json:"offlinecoopmax"`
	OnlineCoopMax  int  `json:"onlinecoopmax"`
	CampaignCoop   bool `json:"campaigncoop"`
	OfflineCoop    bool `json:"offlinecoop"`
	OnlineCoop     bool `json:"onlinecoop"`
}

type igdbAgeRating struct {
	Organization   igdbOrganization   `json:"organization"`
	RatingCategory igdbRatingCategory `json:"rating_category"`
}

type igdbOrganization struct {
	Name string `json:"name"`
}

type igdbRatingCategory struct {
	Rating string `json:"rating"`
}

type igdbImage struct {
	ImageID string `json:"image_id"`
}

type candidate struct {
	game     Game
	igdbID   int
	gameType int
}

type normalizeStats struct {
	platformReleases int
	noUS             int
	noYear           int
	canceled         int
}

func normalizeGames(raw []igdbGame) ([]candidate, normalizeStats) {
	var out []candidate
	var stats normalizeStats

	for _, g := range raw {
		name := strings.TrimSpace(g.Name)
		if g.ID <= 0 || name == "" {
			continue
		}

		for _, p := range supportedPlatforms {
			dates := make([]igdbReleaseDate, 0)
			for _, rd := range g.ReleaseDates {
				if rd.Platform == p.IGDBID {
					dates = append(dates, rd)
				}
			}
			if len(dates) == 0 {
				continue
			}

			stats.platformReleases++
			year, _, ok, canceled := selectUSRelease(dates)
			if canceled {
				stats.canceled++
			}
			if !ok {
				stats.noUS++
				continue
			}
			if year <= 0 {
				stats.noYear++
				continue
			}

			rating, ratingCount := gameRating(g)
			out = append(out, candidate{
				igdbID:   g.ID,
				gameType: g.GameType,
				game: Game{
					ID:          fmt.Sprintf("igdb-%d-%s", g.ID, p.Slug),
					Name:        name,
					Platform:    p.Slug,
					Publisher:   companyNames(g.InvolvedCompanies, func(c igdbInvolvedCompany) bool { return c.Publisher }),
					Developer:   companyNames(g.InvolvedCompanies, func(c igdbInvolvedCompany) bool { return c.Developer }),
					ReleaseYear: year,
					Genres:      genreNames(g.Genres),
					Rating:      rating,
					RatingCount: ratingCount,
					RatingScale: 5,
					MaxPlayers:  maxPlayers(g.MultiplayerModes, p.IGDBID),
					Cooperative: cooperative(g.MultiplayerModes, p.IGDBID),
					ESRB:        esrb(g.AgeRatings),
					Overview:    strings.TrimSpace(g.Summary),
					BoxArt:      coverURL(g.Cover),
					Screenshots: screenshotURLs(g.Screenshots),
				},
			})
		}
	}
	return out, stats
}

type releaseChoice struct {
	key  int64
	year int
}

func selectUSRelease(dates []igdbReleaseDate) (year int, region int, ok bool, canceled bool) {
	var na, worldwide *releaseChoice

	for _, rd := range dates {
		if rd.ReleaseRegion != igdbRegionNorthAmerica && rd.ReleaseRegion != igdbRegionWorldwide {
			continue
		}
		if isCanceledRelease(rd) {
			canceled = true
			continue
		}
		y := releaseYear(rd)
		if y <= 0 {
			continue
		}
		choice := releaseChoice{key: releaseKey(rd), year: y}
		if rd.ReleaseRegion == igdbRegionNorthAmerica {
			if na == nil || choice.key < na.key {
				na = &choice
			}
		} else {
			if worldwide == nil || choice.key < worldwide.key {
				worldwide = &choice
			}
		}
	}

	if na != nil {
		return na.year, igdbRegionNorthAmerica, true, canceled
	}
	if worldwide != nil {
		return worldwide.year, igdbRegionWorldwide, true, canceled
	}
	return 0, 0, false, canceled
}

func isCanceledRelease(rd igdbReleaseDate) bool {
	if rd.Status == nil || rd.Status.Name == "" {
		return false
	}
	name := strings.ToLower(rd.Status.Name)
	return strings.Contains(name, "cancel") || strings.Contains(name, "rumor")
}

func releaseYear(rd igdbReleaseDate) int {
	if rd.Y > 0 {
		return rd.Y
	}
	if rd.Date > 0 {
		return time.Unix(rd.Date, 0).UTC().Year()
	}
	return 0
}

func releaseKey(rd igdbReleaseDate) int64 {
	key := int64(releaseYear(rd)) * 10000
	if rd.Date > 0 {
		t := time.Unix(rd.Date, 0).UTC()
		key += int64(t.Month())*100 + int64(t.Day())
	}
	return key
}

func genreNames(genres []igdbGenre) []string {
	seen := make(map[string]bool, len(genres))
	out := make([]string, 0, len(genres))
	for _, g := range genres {
		name := strings.TrimSpace(g.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	return out
}

func companyNames(companies []igdbInvolvedCompany, include func(igdbInvolvedCompany) bool) string {
	seen := make(map[string]bool, len(companies))
	names := make([]string, 0, len(companies))
	for _, c := range companies {
		if !include(c) {
			continue
		}
		name := strings.TrimSpace(c.Company.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func maxPlayers(modes []igdbMultiplayerMode, platformID int) int {
	best := 0
	for _, m := range modes {
		if m.Platform != platformID && m.Platform != 0 {
			continue
		}
		for _, n := range []int{m.OfflineMax, m.OnlineMax, m.OfflineCoopMax, m.OnlineCoopMax} {
			if n > best {
				best = n
			}
		}
	}
	return best
}

func cooperative(modes []igdbMultiplayerMode, platformID int) bool {
	for _, m := range modes {
		if m.Platform != platformID && m.Platform != 0 {
			continue
		}
		if m.CampaignCoop || m.OfflineCoop || m.OnlineCoop || m.OfflineCoopMax > 1 || m.OnlineCoopMax > 1 {
			return true
		}
	}
	return false
}

func esrb(ratings []igdbAgeRating) string {
	for _, r := range ratings {
		if !strings.Contains(strings.ToLower(r.Organization.Name), "esrb") {
			continue
		}
		if rating := strings.TrimSpace(r.RatingCategory.Rating); rating != "" {
			return rating
		}
	}
	return ""
}

func gameRating(g igdbGame) (float64, int) {
	if g.Rating > 0 && g.RatingCount > 0 {
		return g.Rating / 20, g.RatingCount
	}
	if g.TotalRating > 0 && g.TotalRatingCount > 0 {
		return g.TotalRating / 20, g.TotalRatingCount
	}
	return 0, 0
}

func igdbImageURL(size, imageID string) string {
	if imageID == "" {
		return ""
	}
	return fmt.Sprintf("https://images.igdb.com/igdb/image/upload/%s/%s.jpg", size, imageID)
}

func coverURL(img *igdbImage) string {
	if img == nil {
		return ""
	}
	return igdbImageURL("t_cover_big_2x", img.ImageID)
}

func screenshotURLs(images []igdbImage) []string {
	out := make([]string, 0, maxScreenshots)
	seen := make(map[string]bool, len(images))
	for _, img := range images {
		if img.ImageID == "" || seen[img.ImageID] {
			continue
		}
		seen[img.ImageID] = true
		out = append(out, igdbImageURL("t_screenshot_huge", img.ImageID))
		if len(out) == maxScreenshots {
			break
		}
	}
	return out
}

func dedupeCandidates(candidates []candidate) ([]Game, int) {
	groups := make(map[string][]candidate)
	order := make([]string, 0, len(candidates))
	for _, c := range candidates {
		key := c.game.Platform + "\x00" + normalizeTitle(c.game.Name)
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], c)
	}

	games := make([]Game, 0, len(order))
	merged := 0
	for _, key := range order {
		group := groups[key]
		winner := group[0]
		for _, c := range group[1:] {
			if betterCandidate(c, winner) {
				winner = c
			}
		}
		for _, c := range group {
			if c.game.ID == winner.game.ID {
				continue
			}
			merged++
			log.Printf("igdb: merging %s (%q) into %s", c.game.ID, c.game.Name, winner.game.ID)
		}
		games = append(games, winner.game)
	}
	return games, merged
}

func betterCandidate(a, b candidate) bool {
	aMain := a.gameType == igdbGameTypeMain
	bMain := b.gameType == igdbGameTypeMain
	if aMain != bMain {
		return aMain
	}
	if a.game.RatingCount != b.game.RatingCount {
		return a.game.RatingCount > b.game.RatingCount
	}
	if a.game.Rating != b.game.Rating {
		return a.game.Rating > b.game.Rating
	}
	return a.igdbID < b.igdbID
}

func normalizeTitle(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

func catalogCachePath(cacheDir string) string {
	return filepath.Join(cacheDir, catalogCacheName)
}

func loadCatalogCache(cacheDir string) (*Catalog, time.Time, error) {
	path := catalogCachePath(cacheDir)
	info, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, err
	}

	var games []Game
	if err := json.Unmarshal(data, &games); err != nil {
		return nil, time.Time{}, fmt.Errorf("parse %s: %w", path, err)
	}
	catalog, err := buildCatalog(games)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("validate %s: %w", path, err)
	}
	return catalog, info.ModTime(), nil
}

func writeCatalogCache(cacheDir string, games []Game) error {
	data, err := json.MarshalIndent(games, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(catalogCachePath(cacheDir), data, 0o644)
}

// loadOrFetch returns a fresh cache when possible, otherwise fetches and
// refreshes the cache. A valid stale cache is used when fetching fails.
func loadOrFetch(ctx context.Context, cacheDir string, refresh bool, fetch fetchFunc, logf func(string, ...any)) (*Catalog, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}

	cached, modTime, cacheErr := loadCatalogCache(cacheDir)
	age := time.Since(modTime)
	path := catalogCachePath(cacheDir)

	if cacheErr == nil && !refresh && age < catalogCacheTTL {
		logf("catalog: using cache %s (%s old)", path, age.Round(time.Minute))
		return cached, nil
	}

	if fetch == nil {
		if cacheErr == nil {
			logf("catalog: credentials not set; using stale cache (%s old)", age.Round(time.Minute))
			return cached, nil
		}
		return nil, fmt.Errorf("no usable catalog cache at %s; set IGDB_CLIENT_ID and IGDB_CLIENT_SECRET to fetch", path)
	}

	games, err := fetch(ctx)
	if err != nil {
		if cacheErr == nil {
			logf("catalog: refresh failed (%v); using stale cache (%s old)", err, age.Round(time.Minute))
			return cached, nil
		}
		return nil, fmt.Errorf("fetch IGDB catalog: %w", err)
	}

	catalog, err := buildCatalog(games)
	if err != nil {
		if cacheErr == nil {
			logf("catalog: fetched catalog invalid (%v); using stale cache", err)
			return cached, nil
		}
		return nil, fmt.Errorf("build catalog: %w", err)
	}
	if err := writeCatalogCache(cacheDir, games); err != nil {
		logf("catalog: could not write cache: %v", err)
	} else {
		logf("catalog: wrote %s", path)
	}
	return catalog, nil
}
