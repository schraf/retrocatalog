package main

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const launchBoxFixtureXML = `<?xml version="1.0" encoding="utf-8"?>
<LaunchBox>
  <Game>
    <Name>Super Mario Bros. 3</Name>
    <ReleaseYear>1990</ReleaseYear>
    <Overview>A&nbsp;classic &mdash; really.</Overview>
    <ReleaseType>Released</ReleaseType>
    <DatabaseID>100</DatabaseID>
    <CommunityRating>4.73</CommunityRating>
    <CommunityRatingCount>1106</CommunityRatingCount>
    <Platform>Nintendo Entertainment System</Platform>
  </Game>
  <Game>
    <Name>Unlicensed Game</Name>
    <ReleaseYear>1991</ReleaseYear>
    <ReleaseType>Unlicensed</ReleaseType>
    <DatabaseID>101</DatabaseID>
    <CommunityRating>5</CommunityRating>
    <CommunityRatingCount>50</CommunityRatingCount>
    <Platform>Nintendo Entertainment System</Platform>
  </Game>
  <Game>
    <Name>Homebrew Game</Name>
    <ReleaseYear>2001</ReleaseYear>
    <ReleaseType>Homebrew</ReleaseType>
    <DatabaseID>102</DatabaseID>
    <Platform>Nintendo Game Boy Advance</Platform>
  </Game>
  <Game>
    <Name>Rom Hack</Name>
    <ReleaseYear>2000</ReleaseYear>
    <ReleaseType>ROM Hack</ReleaseType>
    <DatabaseID>103</DatabaseID>
    <Platform>Nintendo 64</Platform>
  </Game>
  <Game>
    <Name>Unreleased Game</Name>
    <ReleaseYear>1995</ReleaseYear>
    <ReleaseType>Unreleased</ReleaseType>
    <DatabaseID>104</DatabaseID>
    <Platform>Super Nintendo Entertainment System</Platform>
  </Game>
  <Game>
    <Name>Game Boy Game</Name>
    <ReleaseDate>1992-01-01</ReleaseDate>
    <ReleaseType>Released</ReleaseType>
    <DatabaseID>105</DatabaseID>
    <CommunityRating></CommunityRating>
    <CommunityRatingCount></CommunityRatingCount>
    <Platform>Nintendo Game Boy</Platform>
  </Game>
  <Game>
    <Name>Bubble Bobble &amp; Friends</Name>
    <ReleaseType>Released</ReleaseType>
    <DatabaseID>106</DatabaseID>
    <Platform>Nintendo Entertainment System</Platform>
  </Game>
  <Game>
    <Name>PC Game</Name>
    <ReleaseYear>1995</ReleaseYear>
    <ReleaseType>Released</ReleaseType>
    <DatabaseID>107</DatabaseID>
    <Platform>Windows</Platform>
  </Game>
</LaunchBox>
`

func TestParseLaunchBoxXML(t *testing.T) {
	games, stats, err := parseLaunchBoxXML(bytes.NewBufferString(launchBoxFixtureXML))
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 8 {
		t.Fatalf("Total = %d, want 8", stats.Total)
	}
	if stats.Kept != 3 {
		t.Fatalf("Kept = %d, want 3", stats.Kept)
	}
	if stats.SkippedType != 4 {
		t.Fatalf("SkippedType = %d, want 4", stats.SkippedType)
	}
	if stats.SkippedPlatform != 1 {
		t.Fatalf("SkippedPlatform = %d, want 1", stats.SkippedPlatform)
	}
	if stats.NoYear != 1 {
		t.Fatalf("NoYear = %d, want 1", stats.NoYear)
	}

	byName := make(map[string]launchBoxGame, len(games))
	for _, game := range games {
		byName[game.Name] = game
	}
	if len(byName) != 3 {
		t.Fatalf("games = %#v", games)
	}
	if game := byName["Super Mario Bros. 3"]; game.DatabaseID != 100 || game.Platform != "nes" ||
		game.ReleaseYear != 1990 || game.Rating != 4.73 || game.RatingCount != 1106 {
		t.Fatalf("SMB3 = %#v", game)
	}
	if game := byName["Game Boy Game"]; game.Platform != "gb" || game.ReleaseYear != 1992 || game.Rating != 0 {
		t.Fatalf("Game Boy Game = %#v", game)
	}
	if _, ok := byName["Bubble Bobble & Friends"]; !ok {
		t.Fatalf("XML entities were not decoded: %#v", games)
	}
}

func TestLaunchBoxPlatformSlug(t *testing.T) {
	tests := map[string]string{
		"Nintendo Entertainment System":        "nes",
		"Nintendo Game Boy":                    "gb",
		"Super Nintendo Entertainment System":  "snes",
		"Nintendo 64":                          "n64",
		"Nintendo Game Boy Color":              "gbc",
		"Nintendo Game Boy Advance":            "gba",
		"Nintendo GameCube":                    "gamecube",
		"Windows":                              "",
		"Nintendo Game Boy Advance (e-Reader)": "",
	}
	for in, want := range tests {
		if got := launchBoxPlatformSlug(in); got != want {
			t.Errorf("launchBoxPlatformSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func writeLaunchBoxZip(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "Metadata.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("Metadata.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(launchBoxFixtureXML)); err != nil {
		t.Fatal(err)
	}
	other, err := writer.Create("Files.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Write([]byte("<Files />")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseLaunchBoxZip(t *testing.T) {
	path := writeLaunchBoxZip(t, t.TempDir())
	games, stats, err := parseLaunchBoxFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Kept != 3 || len(games) != 3 {
		t.Fatalf("zip parse kept %d games, want 3: %#v", stats.Kept, games)
	}
}

func TestLaunchBoxClientLocalFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Metadata.xml")
	if err := os.WriteFile(path, []byte(launchBoxFixtureXML), 0o644); err != nil {
		t.Fatal(err)
	}
	games, err := newLaunchBoxClient(path).fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 3 {
		t.Fatalf("games = %d, want 3", len(games))
	}
}

func TestLaunchBoxClientDownload(t *testing.T) {
	zipPath := writeLaunchBoxZip(t, t.TempDir())
	body, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}

	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(body)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	client := newLaunchBoxClient("")
	client.metadataURL = server.URL
	client.tempDir = tempDir

	games, err := client.fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 3 {
		t.Fatalf("games = %d, want 3", len(games))
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary download was not removed: %#v", entries)
	}
}

func TestLaunchBoxClientDownloadError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := newLaunchBoxClient("")
	client.metadataURL = server.URL
	if _, err := client.fetch(context.Background()); err == nil {
		t.Fatal("expected a download error")
	}
}

func candidateFor(name string, year int, rating float64, count int) launchBoxGame {
	return launchBoxGame{
		DatabaseID:  int(rating*1000) + count,
		Name:        name,
		Platform:    "nes",
		ReleaseYear: year,
		ReleaseType: "Released",
		Rating:      rating,
		RatingCount: count,
	}
}

func TestLaunchBoxMatcherExact(t *testing.T) {
	matcher := newLaunchBoxMatcher("nes", []launchBoxGame{
		candidateFor("Super Mario Bros.", 1985, 4.49, 1238),
		candidateFor("Super Mario Bros. 3", 1990, 4.73, 1106),
	})
	match, kind := matcher.match(testGame("igdb-1-nes", "Super Mario Bros. 3", "nes", 1990))
	if kind != matchExact || match == nil || match.game.Name != "Super Mario Bros. 3" {
		t.Fatalf("match = %#v, kind = %v", match, kind)
	}
}

func TestLaunchBoxMatcherExactUnratedDoesNotFallThrough(t *testing.T) {
	matcher := newLaunchBoxMatcher("nes", []launchBoxGame{
		{Name: "Mystery Game", Platform: "nes", ReleaseYear: 1990, Rating: 0, RatingCount: 0},
		candidateFor("Mystery Game Deluxe", 1990, 4.9, 500),
	})
	match, kind := matcher.match(testGame("igdb-1-nes", "Mystery Game", "nes", 1990))
	if match != nil || kind != matchExact {
		t.Fatalf("match = %#v, kind = %v; want no match", match, kind)
	}
}

func TestLaunchBoxMatcherAlias(t *testing.T) {
	matcher := newLaunchBoxMatcher("snes", []launchBoxGame{
		{Name: "Ninja Gaiden Trilogy", Platform: "snes", ReleaseYear: 1995, Rating: 4.04, RatingCount: 139, DatabaseID: 200},
	})
	match, kind := matcher.match(testGame("igdb-1-snes", "Ninja Gaiden II: The Dark Sword of Chaos", "snes", 1995))
	if kind != matchAlias || match == nil || match.game.DatabaseID != 200 {
		t.Fatalf("match = %#v, kind = %v", match, kind)
	}
}

func TestLaunchBoxMatcherCompilationAliases(t *testing.T) {
	tests := []struct {
		platform string
		title    string
		target   string
		year     int
	}{
		{"gba", "Super Mario Bros. 3", "Super Mario Advance 4: Super Mario Bros. 3", 2003},
		{"gba", "Super Mario World", "Super Mario Advance 2: Super Mario World", 2001},
		{"gba", "Yoshi's Island", "Super Mario Advance 3: Yoshi's Island", 2002},
		{"gbc", "Dragon Warrior I", "Dragon Warrior I & II", 2000},
		{"gbc", "Dragon Warrior II", "Dragon Warrior I & II", 2000},
		{"gbc", "Déjà Vu I", "Déjà Vu I & II: The Casebooks of Ace Harding", 1999},
		{"gbc", "Déjà Vu II", "Déjà Vu I & II: The Casebooks of Ace Harding", 1999},
		{"gb", "Alien Olympics 2044 AD", "Alien Olympics", 1992},
	}
	for _, test := range tests {
		matcher := newLaunchBoxMatcher(test.platform, []launchBoxGame{{
			Name: test.target, Platform: test.platform, ReleaseYear: test.year, Rating: 4, RatingCount: 40,
		}})
		match, kind := matcher.match(testGame("igdb-1-"+test.platform, test.title, test.platform, test.year))
		if kind != matchAlias || match == nil || match.game.Name != test.target {
			t.Errorf("%q: match = %#v, kind = %v", test.title, match, kind)
		}
	}
}

func TestLaunchBoxMatcherFuzzy(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		year     int
		lb       launchBoxGame
	}{
		{"Doom II: Hell on Earth", "gba", 2002, launchBoxGame{Name: "DOOM II", Platform: "gba", ReleaseYear: 2002, Rating: 3.47, RatingCount: 64}},
		{"Terminator 2: Judgment Day", "gb", 1992, launchBoxGame{Name: "T2: Terminator 2: Judgment Day", Platform: "gb", ReleaseYear: 1992, Rating: 2.87, RatingCount: 52}},
		{"Deer Hunter: Interactive Hunting Experience", "gbc", 1999, launchBoxGame{Name: "Deer Hunter", Platform: "gbc", ReleaseYear: 1999, Rating: 2.7, RatingCount: 30}},
	}
	for _, test := range tests {
		matcher := newLaunchBoxMatcher(test.platform, []launchBoxGame{test.lb})
		match, kind := matcher.match(testGame("igdb-1-"+test.platform, test.name, test.platform, test.year))
		if kind != matchFuzzy || match == nil {
			t.Errorf("%q: match = %#v, kind = %v", test.name, match, kind)
		}
	}
}

func TestLaunchBoxMatcherFuzzyRejectsDifferentNumbers(t *testing.T) {
	tests := []struct {
		name string
		lb   launchBoxGame
	}{
		{"Pac-Man", launchBoxGame{Name: "Pac-Man 2: The New Adventures", Platform: "snes", ReleaseYear: 1994, Rating: 4, RatingCount: 100}},
		{"R.B.I. Baseball 2", launchBoxGame{Name: "R.B.I. Baseball", Platform: "nes", ReleaseYear: 1988, Rating: 4, RatingCount: 100}},
		{"F-1 World Grand Prix II", launchBoxGame{Name: "F-1 World Grand Prix", Platform: "gbc", ReleaseYear: 2000, Rating: 4, RatingCount: 100}},
		{"John Madden Football '92", launchBoxGame{Name: "John Madden Football", Platform: "snes", ReleaseYear: 1991, Rating: 4, RatingCount: 100}},
	}
	for _, test := range tests {
		platform := test.lb.Platform
		matcher := newLaunchBoxMatcher(platform, []launchBoxGame{test.lb})
		match, _ := matcher.match(testGame("igdb-1-"+platform, test.name, platform, test.lb.ReleaseYear))
		if match != nil {
			t.Errorf("%q matched %q, want no match", test.name, match.game.Name)
		}
	}
}

func TestLaunchBoxMatcherFuzzyRejectsShortContainment(t *testing.T) {
	matcher := newLaunchBoxMatcher("gamecube", []launchBoxGame{{
		Name: "Ty the Tasmanian Tiger 2: Bush Rescue", Platform: "gamecube",
		ReleaseYear: 2004, Rating: 4, RatingCount: 100,
	}})
	match, _ := matcher.match(testGame("igdb-1-gamecube", "Tiger Rescue", "gamecube", 2004))
	if match != nil {
		t.Fatalf("match = %#v, want nil for a two-word subset of a long title", match)
	}
}

func TestLaunchBoxMatcherNoMatchBlocklist(t *testing.T) {
	tests := []struct {
		platform string
		title    string
		lb       string
		year     int
	}{
		{"gb", "Die Maus: Verrückte Olympiade", "Die Maus", 1997},
		{"gb", "The Blues Brothers: Jukebox Adventure", "The Blues Brothers", 1993},
		{"gba", "DragonBasic Space Invaders", "Space Invaders", 2002},
		{"gba", "Medabots AX: Metabee Version", "Medabots: Metabee", 2002},
		{"gbc", "Tom and Jerry in House Trap", "Tom and Jerry", 1999},
		{"snes", "Uncharted Waters: New Horizons", "New Horizons", 1994},
	}
	for _, test := range tests {
		matcher := newLaunchBoxMatcher(test.platform, []launchBoxGame{{
			Name: test.lb, Platform: test.platform, ReleaseYear: test.year, Rating: 4, RatingCount: 100,
		}})
		match, _ := matcher.match(testGame("igdb-1-"+test.platform, test.title, test.platform, test.year))
		if match != nil {
			t.Errorf("%q matched %q, want no match", test.title, match.game.Name)
		}
	}
}

func TestLaunchBoxMatcherFuzzyRejectsDistantYear(t *testing.T) {
	matcher := newLaunchBoxMatcher("gb", []launchBoxGame{
		{Name: "Alien Olympics", Platform: "gb", ReleaseYear: 1998, Rating: 2.5, RatingCount: 20},
	})
	match, _ := matcher.match(testGame("igdb-1-gb", "Alien Olympics 2044 AD", "gb", 1992))
	if match != nil {
		t.Fatalf("match = %#v, want nil for a six-year gap", match)
	}
}

func TestLaunchBoxMatcherThreeD(t *testing.T) {
	matcher := newLaunchBoxMatcher("snes", []launchBoxGame{
		{Name: "Wolfenstein 3-D", Platform: "snes", ReleaseYear: 1994, Rating: 2.98, RatingCount: 96},
	})
	match, kind := matcher.match(testGame("igdb-1-snes", "Wolfenstein 3D", "snes", 1994))
	if kind != matchExact || match == nil {
		t.Fatalf("match = %#v, kind = %v", match, kind)
	}
}

func TestMergeLaunchBoxRatingsPrimary(t *testing.T) {
	games := []Game{{
		ID: "igdb-1-nes", Name: "Super Mario Bros. 3", Platform: "nes", ReleaseYear: 1990,
		Rating: 4.47, RatingCount: 1747, IGDBRating: 4.47, IGDBRatingCount: 1747,
	}}
	entries := []launchBoxGame{{
		DatabaseID: 100, Name: "Super Mario Bros. 3", Platform: "nes", ReleaseYear: 1990,
		Rating: 4.73, RatingCount: 1106,
	}}

	stats := mergeLaunchBoxRatings(games, entries)
	game := games[0]
	if stats.LaunchBox != 1 || stats.Matched != 1 {
		t.Fatalf("stats = %#v", stats)
	}
	if game.Rating != 4.73 || game.RatingCount != 1106 || game.RatingSource != "launchbox" {
		t.Fatalf("chosen rating = %v/%d from %q", game.Rating, game.RatingCount, game.RatingSource)
	}
	if game.LaunchBoxID != 100 || game.LaunchBoxRating != 4.73 || game.LaunchBoxRatingCount != 1106 {
		t.Fatalf("LaunchBox fields = %#v", game)
	}
	if game.IGDBRating != 4.47 || game.IGDBRatingCount != 1747 {
		t.Fatalf("IGDB fields = %#v", game)
	}
}

func TestMergeLaunchBoxRatingsIGDBFallbackForThinLaunchBox(t *testing.T) {
	games := []Game{{
		ID: "igdb-1-nes", Name: "Contra", Platform: "nes", ReleaseYear: 1988,
		Rating: 3.93, RatingCount: 81, IGDBRating: 3.93, IGDBRatingCount: 81,
	}}
	entries := []launchBoxGame{{
		DatabaseID: 200, Name: "Contra", Platform: "nes", ReleaseYear: 1988,
		Rating: 5, RatingCount: 2,
	}}

	mergeLaunchBoxRatings(games, entries)
	game := games[0]
	if game.RatingSource != "igdb" || game.Rating != 3.93 || game.RatingCount != 81 {
		t.Fatalf("chosen rating = %v/%d from %q", game.Rating, game.RatingCount, game.RatingSource)
	}
	if game.LaunchBoxRatingCount != 2 {
		t.Fatalf("LaunchBox fields should still be kept: %#v", game)
	}
}

func TestMergeLaunchBoxRatingsThinLaunchBoxFillsGap(t *testing.T) {
	games := []Game{{
		ID: "igdb-1-gb", Name: "Tetris", Platform: "gb", ReleaseYear: 1989,
	}}
	entries := []launchBoxGame{{
		DatabaseID: 300, Name: "Tetris", Platform: "gb", ReleaseYear: 1989,
		Rating: 4.52, RatingCount: 3,
	}}

	mergeLaunchBoxRatings(games, entries)
	game := games[0]
	if game.RatingSource != "launchbox" || game.Rating != 4.52 || game.RatingCount != 3 {
		t.Fatalf("chosen rating = %v/%d from %q", game.Rating, game.RatingCount, game.RatingSource)
	}
}

func TestMergeLaunchBoxRatingsUnrated(t *testing.T) {
	games := []Game{{
		ID: "igdb-1-gb", Name: "Obscure Game", Platform: "gb", ReleaseYear: 1995,
	}}
	mergeLaunchBoxRatings(games, nil)
	game := games[0]
	if game.Rating != 0 || game.RatingCount != 0 || game.RatingSource != "" {
		t.Fatalf("unrated game = %#v", game)
	}
}
