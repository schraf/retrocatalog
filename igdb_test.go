package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// newTestIGDBClient points a client at a local test server. The server must
// answer POST /token and POST /games.
func newTestIGDBClient(t *testing.T, handler http.HandlerFunc) (*igdbClient, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &igdbClient{
		clientID:     "test-client",
		clientSecret: "test-secret",
		httpClient:   server.Client(),
		apiBase:      server.URL,
		tokenURL:     server.URL + "/token",
		pageSize:     2,
		requestGap:   0,
		sleep:        func(time.Duration) {},
	}, server
}

func writeToken(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"access_token":"test-token"}`))
}

func TestIGDBClientPagination(t *testing.T) {
	var offsets []int
	offsetPattern := regexp.MustCompile(`offset (\d+);`)

	client, _ := newTestIGDBClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			writeToken(w)
		case "/games":
			if got := r.Header.Get("Client-ID"); got != "test-client" {
				t.Errorf("Client-ID = %q", got)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Errorf("Authorization = %q", got)
			}

			var body strings.Builder
			buf := make([]byte, 4096)
			for {
				n, err := r.Body.Read(buf)
				body.Write(buf[:n])
				if err != nil {
					break
				}
			}
			if !strings.Contains(body.String(), "(game_status = 0 | game_status = null)") {
				t.Errorf("query does not accept null game status: %s", body.String())
			}
			if !strings.Contains(body.String(), "game_type = (0,10,11)") {
				t.Errorf("query does not accept expanded games: %s", body.String())
			}

			match := offsetPattern.FindStringSubmatch(body.String())
			if match == nil {
				t.Errorf("request has no offset: %s", body.String())
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			offset, _ := strconv.Atoi(match[1])
			offsets = append(offsets, offset)

			page := []igdbGame{}
			switch offset {
			case 0:
				page = []igdbGame{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}}
			case 2:
				page = []igdbGame{{ID: 3, Name: "C"}}
			default:
				t.Errorf("unexpected offset %d", offset)
			}
			_ = json.NewEncoder(w).Encode(page)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	})

	games, err := client.fetchGames(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 3 {
		t.Fatalf("games = %d, want 3", len(games))
	}
	if fmt.Sprint(offsets) != "[0 2]" {
		t.Fatalf("offsets = %v, want [0 2]", offsets)
	}
	if client.token != "test-token" {
		t.Fatalf("token = %q", client.token)
	}
}

func TestIGDBClientReauthOn401(t *testing.T) {
	tokenHits, gameHits := 0, 0

	client, _ := newTestIGDBClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenHits++
			writeToken(w)
		case "/games":
			gameHits++
			if gameHits == 1 {
				http.Error(w, "expired", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode([]igdbGame{{ID: 1, Name: "A"}})
		default:
			http.NotFound(w, r)
		}
	})

	if _, err := client.fetchGames(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tokenHits != 2 {
		t.Fatalf("token requests = %d, want 2", tokenHits)
	}
	if gameHits != 2 {
		t.Fatalf("game requests = %d, want 2", gameHits)
	}
}

func TestIGDBClientRetryOn429(t *testing.T) {
	gameHits := 0
	slept := 0

	client, _ := newTestIGDBClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			writeToken(w)
		case "/games":
			gameHits++
			if gameHits == 1 {
				http.Error(w, "slow down", http.StatusTooManyRequests)
				return
			}
			_ = json.NewEncoder(w).Encode([]igdbGame{{ID: 1, Name: "A"}})
		default:
			http.NotFound(w, r)
		}
	})
	client.sleep = func(time.Duration) { slept++ }

	if _, err := client.fetchGames(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gameHits != 2 {
		t.Fatalf("game requests = %d, want 2", gameHits)
	}
	if slept != 1 {
		t.Fatalf("sleeps = %d, want 1", slept)
	}
}

func TestIGDBFetchCatalogZeroGames(t *testing.T) {
	client, _ := newTestIGDBClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			writeToken(w)
		case "/games":
			_ = json.NewEncoder(w).Encode([]igdbGame{})
		default:
			http.NotFound(w, r)
		}
	})

	if _, err := client.fetchCatalog(context.Background()); err == nil || !strings.Contains(err.Error(), "no games") {
		t.Fatalf("expected zero-games error, got %v", err)
	}
}

func TestIGDBFetchCatalog(t *testing.T) {
	client, _ := newTestIGDBClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			writeToken(w)
		case "/games":
			page := []igdbGame{
				{
					ID: 10, Name: "Alpha", GameType: igdbGameTypeMain,
					ReleaseDates: []igdbReleaseDate{
						{Platform: 18, Y: 1987, ReleaseRegion: igdbRegionWorldwide},
						{Platform: 18, Y: 1986, ReleaseRegion: igdbRegionNorthAmerica},
						{Platform: 19, Y: 1991, ReleaseRegion: igdbRegionNorthAmerica},
					},
					Genres:            []igdbGenre{{Name: "Action"}},
					InvolvedCompanies: []igdbInvolvedCompany{{Company: igdbCompany{Name: "Pub"}, Publisher: true}},
					AgeRatings:        []igdbAgeRating{{Organization: igdbOrganization{Name: "ESRB"}, RatingCategory: igdbRatingCategory{Rating: "E"}}},
					Rating:            80, RatingCount: 10,
				},
				{
					ID: 11, Name: "Alpha", GameType: igdbGameTypePort,
					ReleaseDates: []igdbReleaseDate{{Platform: 18, Y: 1986, ReleaseRegion: igdbRegionNorthAmerica}},
					Rating:       90, RatingCount: 1,
				},
			}
			_ = json.NewEncoder(w).Encode(page)
		default:
			http.NotFound(w, r)
		}
	})
	client.pageSize = 100

	games, err := client.fetchCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 2 {
		t.Fatalf("games = %d, want 2: %#v", len(games), games)
	}
	if games[0].ID != "igdb-10-nes" || games[0].ReleaseYear != 1986 {
		t.Fatalf("nes game = %#v", games[0])
	}
	if games[0].Rating != 4 || games[0].RatingCount != 10 || games[0].ESRB != "E" {
		t.Fatalf("nes rating/esrb = %#v", games[0])
	}
	if games[1].ID != "igdb-10-snes" || games[1].ReleaseYear != 1991 {
		t.Fatalf("snes game = %#v", games[1])
	}
}

func TestSelectUSRelease(t *testing.T) {
	tests := []struct {
		name     string
		dates    []igdbReleaseDate
		year     int
		region   int
		ok       bool
		canceled bool
	}{
		{
			name: "prefers north america over worldwide",
			dates: []igdbReleaseDate{
				{Platform: 18, Y: 1987, ReleaseRegion: igdbRegionWorldwide},
				{Platform: 18, Y: 1986, ReleaseRegion: igdbRegionNorthAmerica},
			},
			year: 1986, region: igdbRegionNorthAmerica, ok: true,
		},
		{
			name: "uses worldwide when no north america date",
			dates: []igdbReleaseDate{
				{Platform: 18, Y: 1988, ReleaseRegion: igdbRegionWorldwide},
			},
			year: 1988, region: igdbRegionWorldwide, ok: true,
		},
		{
			name: "takes earliest north america date",
			dates: []igdbReleaseDate{
				{Platform: 18, Y: 1990, ReleaseRegion: igdbRegionNorthAmerica},
				{Platform: 18, Y: 1989, ReleaseRegion: igdbRegionNorthAmerica},
			},
			year: 1989, region: igdbRegionNorthAmerica, ok: true,
		},
		{
			name: "compares full dates with year-only dates",
			dates: []igdbReleaseDate{
				{Platform: 18, Date: time.Date(1988, time.March, 1, 0, 0, 0, 0, time.UTC).Unix(), ReleaseRegion: igdbRegionNorthAmerica},
				{Platform: 18, Y: 1987, ReleaseRegion: igdbRegionNorthAmerica},
			},
			year: 1987, region: igdbRegionNorthAmerica, ok: true,
		},
		{
			name: "skips other regions",
			dates: []igdbReleaseDate{
				{Platform: 18, Y: 1987, ReleaseRegion: 1},
			},
		},
		{
			name: "skips canceled dates",
			dates: []igdbReleaseDate{
				{Platform: 18, Y: 1987, ReleaseRegion: igdbRegionNorthAmerica, Status: &igdbReleaseStatus{Name: "Cancelled"}},
			},
			canceled: true,
		},
		{
			name: "falls back to unix date year",
			dates: []igdbReleaseDate{
				{Platform: 18, Date: time.Date(1985, time.October, 18, 0, 0, 0, 0, time.UTC).Unix(), ReleaseRegion: igdbRegionNorthAmerica},
			},
			year: 1985, region: igdbRegionNorthAmerica, ok: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			year, region, ok, canceled := selectUSRelease(test.dates)
			if year != test.year || region != test.region || ok != test.ok || canceled != test.canceled {
				t.Fatalf("selectUSRelease() = %d, %d, %v, %v; want %d, %d, %v, %v",
					year, region, ok, canceled, test.year, test.region, test.ok, test.canceled)
			}
		})
	}
}

func TestNormalizeGamesFanOut(t *testing.T) {
	raw := []igdbGame{
		{
			ID:       10,
			Name:     " Alpha ",
			GameType: igdbGameTypeMain,
			ReleaseDates: []igdbReleaseDate{
				{Platform: 18, Y: 1986, ReleaseRegion: igdbRegionNorthAmerica},
				{Platform: 19, Y: 1991, ReleaseRegion: igdbRegionNorthAmerica},
				{Platform: 4, Y: 1996, ReleaseRegion: igdbRegionNorthAmerica},
				{Platform: 99, Y: 1990, ReleaseRegion: igdbRegionNorthAmerica},
			},
			Genres: []igdbGenre{{Name: "Action"}, {Name: "action"}, {Name: "Platformer"}},
			InvolvedCompanies: []igdbInvolvedCompany{
				{Company: igdbCompany{Name: "Studio B"}, Developer: true},
				{Company: igdbCompany{Name: "Pub A"}, Publisher: true},
			},
			MultiplayerModes: []igdbMultiplayerMode{
				{Platform: 18, OfflineMax: 2, OfflineCoopMax: 2, CampaignCoop: true},
				{Platform: 19, OnlineMax: 4},
			},
			AgeRatings:  []igdbAgeRating{{Organization: igdbOrganization{Name: "ESRB"}, RatingCategory: igdbRatingCategory{Rating: "E"}}},
			Rating:      80,
			RatingCount: 10,
			Summary:     " Summary ",
			Cover:       &igdbImage{ImageID: "co1"},
			Screenshots: []igdbImage{
				{ImageID: "sc1"},
				{ImageID: "sc2"},
				{ImageID: "sc1"},
				{ImageID: "sc3"},
				{ImageID: "sc4"},
			},
		},
	}

	candidates, stats := normalizeGames(raw)
	if len(candidates) != 3 {
		t.Fatalf("candidates = %d, want 3 (only supported platforms): %#v", len(candidates), candidates)
	}
	if stats.platformReleases != 3 {
		t.Fatalf("platformReleases = %d, want 3", stats.platformReleases)
	}

	nes := candidates[0].game
	if nes.ID != "igdb-10-nes" || nes.Name != "Alpha" || nes.Platform != "nes" {
		t.Fatalf("nes = %#v", nes)
	}
	if nes.ReleaseYear != 1986 {
		t.Fatalf("nes year = %d", nes.ReleaseYear)
	}
	if nes.Publisher != "Pub A" || nes.Developer != "Studio B" {
		t.Fatalf("companies = %q / %q", nes.Publisher, nes.Developer)
	}
	if fmt.Sprint(nes.Genres) != "[Action Platformer]" {
		t.Fatalf("genres = %#v", nes.Genres)
	}
	if nes.Rating != 4 || nes.RatingCount != 10 || nes.RatingScale != 5 {
		t.Fatalf("rating = %#v", nes)
	}
	if nes.MaxPlayers != 2 || !nes.Cooperative {
		t.Fatalf("multiplayer = max %d coop %v", nes.MaxPlayers, nes.Cooperative)
	}
	if nes.ESRB != "E" || nes.Overview != "Summary" {
		t.Fatalf("esrb/overview = %q / %q", nes.ESRB, nes.Overview)
	}
	if nes.BoxArt != "https://images.igdb.com/igdb/image/upload/t_cover_big_2x/co1.jpg" {
		t.Fatalf("box art = %q", nes.BoxArt)
	}
	if got, want := fmt.Sprint(nes.Screenshots), "[https://images.igdb.com/igdb/image/upload/t_screenshot_huge/sc1.jpg https://images.igdb.com/igdb/image/upload/t_screenshot_huge/sc2.jpg https://images.igdb.com/igdb/image/upload/t_screenshot_huge/sc3.jpg]"; got != want {
		t.Fatalf("screenshots = %s, want %s", got, want)
	}

	snes := candidates[1].game
	if snes.ID != "igdb-10-snes" || snes.ReleaseYear != 1991 {
		t.Fatalf("snes = %#v", snes)
	}
	if snes.MaxPlayers != 4 || snes.Cooperative {
		t.Fatalf("snes multiplayer = max %d coop %v", snes.MaxPlayers, snes.Cooperative)
	}

	n64 := candidates[2].game
	if n64.ID != "igdb-10-n64" || n64.ReleaseYear != 1996 {
		t.Fatalf("n64 = %#v", n64)
	}
}

func TestGameRatingFallback(t *testing.T) {
	if rating, count := gameRating(igdbGame{Rating: 80, RatingCount: 10, TotalRating: 90, TotalRatingCount: 20}); rating != 4 || count != 10 {
		t.Fatalf("preferred rating = %v / %d", rating, count)
	}
	if rating, count := gameRating(igdbGame{TotalRating: 75, TotalRatingCount: 20}); rating != 3.75 || count != 20 {
		t.Fatalf("fallback rating = %v / %d", rating, count)
	}
	if rating, count := gameRating(igdbGame{}); rating != 0 || count != 0 {
		t.Fatalf("empty rating = %v / %d", rating, count)
	}
}

func TestMultiplayerHelpers(t *testing.T) {
	modes := []igdbMultiplayerMode{
		{Platform: 18, OfflineMax: 2, OfflineCoopMax: 2},
		{Platform: 19, OnlineMax: 8},
		{Platform: 0, OfflineMax: 4},
	}

	if got := maxPlayers(modes, 18); got != 4 {
		t.Fatalf("maxPlayers(nes) = %d, want 4", got)
	}
	if got := maxPlayers(modes, 19); got != 8 {
		t.Fatalf("maxPlayers(snes) = %d, want 8", got)
	}
	// Platform zero applies to every platform.
	if got := maxPlayers(modes, 21); got != 4 {
		t.Fatalf("maxPlayers(gamecube) = %d, want 4", got)
	}
	if !cooperative(modes, 18) {
		t.Fatal("nes should be cooperative from offlinecoopmax")
	}
	if cooperative(modes, 19) {
		t.Fatal("snes should not be cooperative")
	}
}

func TestDedupeCandidates(t *testing.T) {
	makeCandidate := func(id int, platform, name string, gameType, count int) candidate {
		return candidate{
			igdbID:   id,
			gameType: gameType,
			game: Game{
				ID:          fmt.Sprintf("igdb-%d-%s", id, platform),
				Name:        name,
				Platform:    platform,
				RatingCount: count,
			},
		}
	}

	candidates := []candidate{
		makeCandidate(3, "nes", "Alpha", igdbGameTypePort, 100),
		makeCandidate(2, "nes", " alpha ", igdbGameTypeMain, 1),
		makeCandidate(1, "nes", "ALPHA", igdbGameTypeMain, 1),
		makeCandidate(4, "snes", "Alpha", igdbGameTypePort, 1),
	}

	games, merged := dedupeCandidates(candidates)
	if merged != 2 {
		t.Fatalf("merged = %d, want 2", merged)
	}
	if len(games) != 2 {
		t.Fatalf("games = %d, want 2", len(games))
	}
	// Among main_game entries, the lower IGDB ID wins when the rest is equal.
	if games[0].ID != "igdb-1-nes" {
		t.Fatalf("nes winner = %q, want igdb-1-nes", games[0].ID)
	}
	if games[1].ID != "igdb-4-snes" {
		t.Fatalf("snes winner = %q, want igdb-4-snes", games[1].ID)
	}
}

func TestLoadOrFetchCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	games := []Game{
		testGame("igdb-1-nes", "Alpha", "nes", 1986, "Action"),
		testGame("igdb-2-snes", "Beta", "snes", 1991, "Shooter"),
	}

	calls := 0
	fetch := func(context.Context) ([]Game, error) {
		calls++
		return games, nil
	}

	catalog, err := loadOrFetch(context.Background(), dir, false, fetch, nil)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Total() != 2 || calls != 1 {
		t.Fatalf("total = %d, calls = %d", catalog.Total(), calls)
	}
	if _, err := os.Stat(catalogCachePath(dir)); err != nil {
		t.Fatalf("cache not written: %v", err)
	}

	// A fresh cache is used without fetching, even when fetch would fail.
	catalog, err = loadOrFetch(context.Background(), dir, false, func(context.Context) ([]Game, error) {
		t.Fatal("fetch called for fresh cache")
		return nil, errors.New("unexpected")
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Total() != 2 {
		t.Fatalf("cached total = %d, want 2", catalog.Total())
	}
}

func TestLoadOrFetchStaleCacheFallsBack(t *testing.T) {
	dir := t.TempDir()
	games := []Game{testGame("igdb-1-nes", "Alpha", "nes", 1986, "Action")}
	if _, err := loadOrFetch(context.Background(), dir, false, func(context.Context) ([]Game, error) {
		return games, nil
	}, nil); err != nil {
		t.Fatal(err)
	}

	stale := time.Now().Add(-catalogCacheTTL - time.Hour)
	if err := os.Chtimes(catalogCachePath(dir), stale, stale); err != nil {
		t.Fatal(err)
	}

	fetched := false
	catalog, err := loadOrFetch(context.Background(), dir, false, func(context.Context) ([]Game, error) {
		fetched = true
		return nil, errors.New("network down")
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !fetched {
		t.Fatal("stale cache did not trigger a fetch")
	}
	if catalog.Total() != 1 {
		t.Fatalf("fallback total = %d, want 1", catalog.Total())
	}
}

func TestLoadOrFetchRefreshBypassesFreshCache(t *testing.T) {
	dir := t.TempDir()
	if _, err := loadOrFetch(context.Background(), dir, false, func(context.Context) ([]Game, error) {
		return []Game{testGame("igdb-1-nes", "Alpha", "nes", 1986, "Action")}, nil
	}, nil); err != nil {
		t.Fatal(err)
	}

	fetched := false
	catalog, err := loadOrFetch(context.Background(), dir, true, func(context.Context) ([]Game, error) {
		fetched = true
		return []Game{
			testGame("igdb-1-nes", "Alpha", "nes", 1986, "Action"),
			testGame("igdb-3-n64", "Gamma", "n64", 1996, "Action"),
		}, nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !fetched {
		t.Fatal("refresh did not fetch")
	}
	if catalog.Total() != 2 {
		t.Fatalf("total = %d, want 2", catalog.Total())
	}
}

func TestLoadOrFetchRequiresCacheOrCredentials(t *testing.T) {
	dir := t.TempDir()
	if _, err := loadOrFetch(context.Background(), dir, false, nil, nil); err == nil || !strings.Contains(err.Error(), "IGDB_CLIENT_ID") {
		t.Fatalf("expected credentials error, got %v", err)
	}

	if _, err := loadOrFetch(context.Background(), dir, false, func(context.Context) ([]Game, error) {
		return nil, errors.New("network down")
	}, nil); err == nil || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("expected fetch error, got %v", err)
	}
}

func TestLoadOrFetchRejectsInvalidCache(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(catalogCachePath(dir), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	catalog, err := loadOrFetch(context.Background(), dir, false, func(context.Context) ([]Game, error) {
		return []Game{testGame("igdb-1-nes", "Alpha", "nes", 1986, "Action")}, nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Total() != 1 {
		t.Fatalf("total = %d, want 1", catalog.Total())
	}

	// The invalid cache was replaced with valid JSON.
	if _, _, err := loadCatalogCache(dir); err != nil {
		t.Fatalf("cache was not repaired: %v", err)
	}
}

func TestCatalogCachePath(t *testing.T) {
	if got, want := catalogCachePath("/tmp/cache"), filepath.Join("/tmp/cache", "catalog-v2.json"); got != want {
		t.Fatalf("catalogCachePath = %q, want %q", got, want)
	}
}
