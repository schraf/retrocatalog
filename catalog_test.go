package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

const testCatalog = `[
  {
    "name": "Alpha",
    "publisher": "Pub A",
    "developer": "Dev A",
    "players": 2,
    "release_year": 1986,
    "genre": "Platformer",
    "tags": ["Platform"],
    "rating": 4.1,
    "rating_count": 100,
    "rating_scale": 5,
    "max_players": 2,
    "cooperative": true,
    "esrb": "E - Everyone",
    "overview": "First game.",
    "launchbox_id": 1,
    "box_art": "https://images.launchbox-app.com/alpha.jpg",
    "screenshot": "https://images.launchbox-app.com/alpha.png"
  },
  {
    "name": "Beta",
    "publisher": "Pub B",
    "developer": "Dev B",
    "players": 1,
    "release_year": 1991,
    "genre": "Shooter",
    "tags": ["Shooter"],
    "rating": 3.2,
    "rating_count": 50,
    "rating_scale": 5,
    "max_players": 1,
    "cooperative": false,
    "esrb": "Not Rated",
    "overview": "Second game.",
    "launchbox_id": 2,
    "box_art": "https://thumbnails.libretro.com/beta.png",
    "screenshot": "https://images.launchbox-app.com/beta.jpg"
  }
]`

func writeCatalog(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "games.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadCatalog(t *testing.T) {
	catalog, err := LoadCatalog(writeCatalog(t, testCatalog))
	if err != nil {
		t.Fatal(err)
	}

	if catalog.Total() != 2 {
		t.Fatalf("Total() = %d, want 2", catalog.Total())
	}
	if game := catalog.ByID(2); game == nil || game.Name != "Beta" {
		t.Fatalf("ByID(2) = %#v", game)
	}
	if catalog.ByID(99) != nil {
		t.Fatal("ByID(99) should be nil")
	}
	if catalog.yearMin != 1986 || catalog.yearMax != 1991 {
		t.Fatalf("year range = %d-%d, want 1986-1991", catalog.yearMin, catalog.yearMax)
	}
	if catalog.ratingMin != 3.2 || catalog.ratingMax != 4.1 {
		t.Fatalf("rating range = %v-%v, want 3.2-4.1", catalog.ratingMin, catalog.ratingMax)
	}

	genres := catalog.Genres()
	if len(genres) != 2 || genres[0].Genre != "Platformer" || genres[1].Genre != "Shooter" {
		t.Fatalf("genres = %#v", genres)
	}

	index := catalog.Index()
	if len(index) != 2 || index[0].ID != 1 || index[0].Year != 1986 {
		t.Fatalf("index = %#v", index)
	}
	if math.Abs(catalog.ratingMean-3.8) > 1e-9 {
		t.Fatalf("ratingMean = %v, want 3.8", catalog.ratingMean)
	}
	if math.Abs(index[0].WeightedRating-695.0/175.0) > 1e-9 {
		t.Fatalf("Alpha weighted rating = %v, want %v", index[0].WeightedRating, 695.0/175.0)
	}
	if math.Abs(index[1].WeightedRating-3.56) > 1e-9 {
		t.Fatalf("Beta weighted rating = %v, want 3.56", index[1].WeightedRating)
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

func TestLoadCatalogRejectsDuplicateIDs(t *testing.T) {
	duplicate := `[
      {"name":"A","genre":"Action","launchbox_id":7,
       "box_art":"https://images.launchbox-app.com/a.jpg",
       "screenshot":"https://images.launchbox-app.com/a.png"},
      {"name":"B","genre":"Action","launchbox_id":7,
       "box_art":"https://images.launchbox-app.com/b.jpg",
       "screenshot":"https://images.launchbox-app.com/b.png"}
    ]`
	if _, err := LoadCatalog(writeCatalog(t, duplicate)); err == nil {
		t.Fatal("expected duplicate launchbox_id error")
	}
}

func TestLoadCatalogRejectsForeignArtHost(t *testing.T) {
	foreign := `[{"name":"A","genre":"Action","launchbox_id":1,
      "box_art":"https://example.com/a.jpg",
      "screenshot":"https://images.launchbox-app.com/a.png"}]`
	if _, err := LoadCatalog(writeCatalog(t, foreign)); err == nil {
		t.Fatal("expected disallowed art host error")
	}
}

func TestLoadCatalogRejectsMissingFile(t *testing.T) {
	if _, err := LoadCatalog(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected read error")
	}
}
