package main

import "testing"

func TestAction52ExclusionList(t *testing.T) {
	if got := len(excludedTitles["nes"]); got != 52 {
		t.Fatalf("Action 52 list has %d entries, want 52", got)
	}
	for _, name := range []string{"Cheetahmen", "Atmos Quake", "Underground", "They Came..."} {
		if !isExcludedGame(testGame("igdb-1-nes", name, "nes", 1991)) {
			t.Errorf("%q should be excluded", name)
		}
	}
}

func TestIsExcludedGame(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		excluded bool
	}{
		{"Cheetahmen", "nes", true},
		{"Cheetahmen", "snes", false},
		{"Super Mario Bros. 3", "nes", false},
		{"Dyna Blade", "snes", true},
		{"Kirby Super Star", "snes", false},
		{"King James Bible for Use on Game Boy", "gb", true},
		{"Pocket Professor: Kwik Notes Volume 1", "gba", true},
		{"Welcome to GameCube World!!", "gamecube", true},
		{"Resident Evil 4: GameStop Special Edition", "gamecube", true},
		{"Resident Evil 4", "gamecube", false},
		{"Nintendo Campus Challenge 1992", "snes", true},
		{"Chrono Trigger", "snes", false},
	}
	for _, test := range tests {
		game := testGame("igdb-1-"+test.platform, test.name, test.platform, 1995)
		if got := isExcludedGame(game); got != test.excluded {
			t.Errorf("isExcludedGame(%q, %s) = %v, want %v", test.name, test.platform, got, test.excluded)
		}
	}
}

func TestFilterExcludedGames(t *testing.T) {
	games := []Game{
		testGame("igdb-1-nes", "Cheetahmen", "nes", 1991),
		testGame("igdb-2-nes", "Super Mario Bros. 3", "nes", 1990),
		testGame("igdb-3-snes", "Dyna Blade", "snes", 1996),
		testGame("igdb-4-snes", "Chrono Trigger", "snes", 1995),
	}
	filtered, removed := filterExcludedGames(games)
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	if len(filtered) != 2 {
		t.Fatalf("filtered = %d, want 2", len(filtered))
	}
	for _, game := range filtered {
		if isExcludedGame(game) {
			t.Errorf("%q should have been removed", game.Name)
		}
	}
}

func TestBuildCatalogExcludesArtifacts(t *testing.T) {
	games := []Game{
		testGame("igdb-1-nes", "Cheetahmen", "nes", 1991, "Action"),
		testGame("igdb-2-nes", "Super Mario Bros. 3", "nes", 1990, "Platformer"),
		testGame("igdb-3-snes", "Dyna Blade", "snes", 1996, "Platformer"),
		testGame("igdb-4-gb", "King James Bible for Use on Game Boy", "gb", 1994),
		testGame("igdb-5-snes", "Chrono Trigger", "snes", 1995, "Role-playing (RPG)"),
	}

	catalog, err := buildCatalog(games)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Total() != 2 {
		t.Fatalf("Total() = %d, want 2", catalog.Total())
	}
	for _, id := range []string{"igdb-1-nes", "igdb-3-snes", "igdb-4-gb"} {
		if catalog.ByID(id) != nil {
			t.Errorf("ByID(%s) should be nil", id)
		}
	}
	for _, id := range []string{"igdb-2-nes", "igdb-5-snes"} {
		if catalog.ByID(id) == nil {
			t.Errorf("ByID(%s) should be present", id)
		}
	}
}
