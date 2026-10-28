package main

import (
	"math"
	"strings"
	"testing"
)

func testGame(id, name, platform string, year int, genres ...string) Game {
	return Game{
		ID:          id,
		Name:        name,
		Platform:    platform,
		ReleaseYear: year,
		Genres:      genres,
		RatingScale: 5,
		BoxArt:      "https://images.igdb.com/igdb/image/upload/t_cover_big_2x/" + id + ".jpg",
	}
}

func TestBuildCatalog(t *testing.T) {
	games := []Game{
		{
			ID: "igdb-1-nes", Name: "Alpha", Platform: "nes", ReleaseYear: 1986,
			Publisher: "Pub A", Developer: "Dev A",
			Genres: []string{"Platformer", "Action", "action"},
			Rating: 4.1, RatingCount: 100, RatingScale: 5,
			MaxPlayers: 2, Cooperative: true, ESRB: "E",
			Overview: "First game.",
			BoxArt:   "https://images.igdb.com/igdb/image/upload/t_cover_big_2x/alpha.jpg",
			Screenshots: []string{
				"https://images.igdb.com/igdb/image/upload/t_screenshot_huge/alpha.png",
			},
		},
		{
			ID: "igdb-2-snes", Name: "Beta", Platform: "snes", ReleaseYear: 1991,
			Publisher: "Pub B", Developer: "Dev B",
			Genres: []string{"Shooter"},
			Rating: 3.2, RatingCount: 50, RatingScale: 5,
			BoxArt: "https://images.igdb.com/igdb/image/upload/t_cover_big_2x/beta.jpg",
		},
		{
			ID: "igdb-3-n64", Name: "Gamma", Platform: "n64", ReleaseYear: 1996,
			Genres: []string{"Platformer"},
			BoxArt: "https://images.igdb.com/igdb/image/upload/t_cover_big_2x/gamma.jpg",
		},
	}

	catalog, err := buildCatalog(games)
	if err != nil {
		t.Fatal(err)
	}

	if catalog.Total() != 3 {
		t.Fatalf("Total() = %d, want 3", catalog.Total())
	}
	if game := catalog.ByID("igdb-2-snes"); game == nil || game.Name != "Beta" {
		t.Fatalf("ByID(igdb-2-snes) = %#v", game)
	}
	if catalog.ByID("igdb-99-nes") != nil {
		t.Fatal("ByID(igdb-99-nes) should be nil")
	}
	if catalog.yearMin != 1986 || catalog.yearMax != 1996 {
		t.Fatalf("year range = %d-%d, want 1986-1996", catalog.yearMin, catalog.yearMax)
	}
	if catalog.ratingMin != 3.2 || catalog.ratingMax != 4.1 {
		t.Fatalf("rating range = %v-%v, want 3.2-4.1 for rated games only", catalog.ratingMin, catalog.ratingMax)
	}

	genres := catalog.Genres()
	if len(genres) != 3 {
		t.Fatalf("genres = %#v", genres)
	}
	if genres[0].Genre != "Platformer" || genres[0].Count != 2 {
		t.Fatalf("genres[0] = %#v", genres[0])
	}
	if genres[1].Genre != "Action" || genres[2].Genre != "Shooter" {
		t.Fatalf("genres = %#v", genres)
	}

	platforms := catalog.Platforms()
	if len(platforms) != len(supportedPlatforms) {
		t.Fatalf("platforms = %#v", platforms)
	}
	wantCounts := map[string]int{"nes": 1, "snes": 1, "n64": 1}
	for _, p := range platforms {
		if p.Count != wantCounts[p.Slug] {
			t.Errorf("platform %s count = %d, want %d", p.Slug, p.Count, wantCounts[p.Slug])
		}
	}

	index := catalog.Index()
	if len(index) != 3 || index[0].ID != "igdb-1-nes" || index[0].Year != 1986 {
		t.Fatalf("index = %#v", index)
	}
	if len(index[0].Genres) != 2 || index[0].Genres[0] != "Platformer" || index[0].Genres[1] != "Action" {
		t.Fatalf("index[0].Genres = %#v", index[0].Genres)
	}
	if math.Abs(catalog.ratingMean-3.8) > 1e-9 {
		t.Fatalf("ratingMean = %v, want 3.8", catalog.ratingMean)
	}
	if math.Abs(catalog.ratingMeans["nes"]-4.1) > 1e-9 || math.Abs(catalog.ratingMeans["snes"]-3.2) > 1e-9 {
		t.Fatalf("platform means = %v, want nes 4.1 snes 3.2", catalog.ratingMeans)
	}
	if math.Abs(index[0].WeightedRating-4.1) > 1e-9 {
		t.Fatalf("Alpha weighted rating = %v, want 4.1", index[0].WeightedRating)
	}
	if index[0].BoxArt != games[0].BoxArt {
		t.Fatalf("index[0].BoxArt = %q, want %q", index[0].BoxArt, games[0].BoxArt)
	}
	if math.Abs(index[1].WeightedRating-3.2) > 1e-9 {
		t.Fatalf("Beta weighted rating = %v, want 3.2", index[1].WeightedRating)
	}
	if index[2].WeightedRating != 0 {
		t.Fatalf("Gamma weighted rating = %v, want 0 for an unrated game", index[2].WeightedRating)
	}
}

func TestWeightedRating(t *testing.T) {
	const mean = 3.566

	thin := weightedRating(4.34, 19, mean)    // Punch-Out!!
	steady := weightedRating(4.23, 704, mean) // Castlevania
	top := weightedRating(4.73, 1105, mean)   // Super Mario Bros. 3

	if thin >= steady {
		t.Fatalf("thin high rating (%v) should rank below a well-rated game (%v)", thin, steady)
	}
	if top <= steady {
		t.Fatalf("top weighted rating (%v) should rank above %v", top, steady)
	}
	if top >= 4.73 {
		t.Fatalf("weighted rating (%v) should be below the raw rating 4.73", top)
	}
	if got := weightedRating(3.0, 0, mean); got != mean {
		t.Fatalf("weightedRating with no votes = %v, want %v", got, mean)
	}
}

func TestBuildCatalogNormalizesScreenshots(t *testing.T) {
	const base = "https://images.igdb.com/igdb/image/upload/t_screenshot_huge/"
	game := testGame("igdb-1-nes", "A", "nes", 1986, "Action")
	game.Screenshots = []string{
		base + "one.jpg",
		"",
		base + "one.jpg",
		base + "two.jpg",
		base + "three.jpg",
		base + "four.jpg",
	}

	catalog, err := buildCatalog([]Game{game})
	if err != nil {
		t.Fatal(err)
	}
	got := catalog.ByID("igdb-1-nes")
	want := []string{base + "one.jpg", base + "two.jpg", base + "three.jpg"}
	if got == nil || strings.Join(got.Screenshots, ",") != strings.Join(want, ",") {
		t.Fatalf("screenshots = %#v, want %#v", got, want)
	}
}

func TestBuildCatalogRejectsForeignScreenshotHost(t *testing.T) {
	game := testGame("igdb-1-nes", "A", "nes", 1986, "Action")
	game.Screenshots = []string{"https://example.com/a.jpg"}
	if _, err := buildCatalog([]Game{game}); err == nil {
		t.Fatal("expected disallowed art host error")
	}
}

func TestBuildCatalogRejectsDuplicateIDs(t *testing.T) {
	games := []Game{
		testGame("igdb-7-nes", "A", "nes", 1986, "Action"),
		testGame("igdb-7-nes", "B", "nes", 1987, "Action"),
	}
	if _, err := buildCatalog(games); err == nil || !strings.Contains(err.Error(), "duplicate id") {
		t.Fatalf("expected duplicate id error, got %v", err)
	}
}

func TestBuildCatalogRejectsUnknownPlatform(t *testing.T) {
	games := []Game{testGame("igdb-1-ngage", "A", "ngage", 2005, "Action")}
	if _, err := buildCatalog(games); err == nil {
		t.Fatal("expected invalid id error")
	}
}

func TestBuildCatalogRejectsIDPlatformMismatch(t *testing.T) {
	games := []Game{testGame("igdb-1-nes", "A", "snes", 1986, "Action")}
	if _, err := buildCatalog(games); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected id/platform mismatch error, got %v", err)
	}
}

func TestBuildCatalogRejectsForeignArtHost(t *testing.T) {
	game := testGame("igdb-1-nes", "A", "nes", 1986, "Action")
	game.BoxArt = "https://example.com/a.jpg"
	if _, err := buildCatalog([]Game{game}); err == nil {
		t.Fatal("expected disallowed art host error")
	}
}

func TestBuildCatalogExcludesLateReleases(t *testing.T) {
	games := []Game{
		testGame("igdb-1-nes", "Last Licensed", "nes", 1994, "Action"),
		testGame("igdb-2-nes", "Homebrew", "nes", 1995, "Action"),
		testGame("igdb-3-gamecube", "Last Licensed", "gamecube", 2007, "Action"),
		testGame("igdb-4-gamecube", "Unreleased", "gamecube", 2008, "Action"),
	}

	catalog, err := buildCatalog(games)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Total() != 2 {
		t.Fatalf("Total() = %d, want 2", catalog.Total())
	}
	for _, id := range []string{"igdb-2-nes", "igdb-4-gamecube"} {
		if catalog.ByID(id) != nil {
			t.Errorf("ByID(%s) should be nil", id)
		}
	}
}

func TestBuildCatalogRequiresBoxArt(t *testing.T) {
	games := []Game{
		testGame("igdb-1-nes", "With Art", "nes", 1990, "Action"),
		testGame("igdb-2-nes", "No Art", "nes", 1990, "Action"),
	}
	games[1].BoxArt = ""

	catalog, err := buildCatalog(games)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Total() != 1 {
		t.Fatalf("Total() = %d, want 1", catalog.Total())
	}
	if catalog.ByID("igdb-2-nes") != nil {
		t.Fatal("ByID(igdb-2-nes) should be nil")
	}
}

func TestBuildCatalogRejectsEmpty(t *testing.T) {
	if _, err := buildCatalog(nil); err == nil {
		t.Fatal("expected empty catalog error")
	}
}

func TestBuildCatalogAllowsSameNameOnDifferentPlatforms(t *testing.T) {
	games := []Game{
		testGame("igdb-1-nes", "Same", "nes", 1986, "Action"),
		testGame("igdb-1-snes", "Same", "snes", 1991, "Action"),
	}
	catalog, err := buildCatalog(games)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Total() != 2 {
		t.Fatalf("Total() = %d, want 2", catalog.Total())
	}
}

func TestBuildCatalogAcceptsMissingOptionalFields(t *testing.T) {
	games := []Game{testGame("igdb-1-gb", "Minimal", "gb", 1990)}
	catalog, err := buildCatalog(games)
	if err != nil {
		t.Fatal(err)
	}
	game := catalog.ByID("igdb-1-gb")
	if game == nil {
		t.Fatal("missing game")
	}
	if game.Rating != 0 || game.RatingCount != 0 || game.MaxPlayers != 0 || game.Cooperative || game.ESRB != "" {
		t.Fatalf("optional fields = %#v", game)
	}
	if game.Genres == nil || len(game.Genres) != 0 {
		t.Fatalf("genres = %#v, want empty non-nil slice", game.Genres)
	}
}
