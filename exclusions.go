package main

// The catalog is a curated set of commercial retail releases. A handful of
// IGDB records are components of a compilation, promotional cartridges, or
// non-game software, and they have always been noise in the grid. Exclusions
// are made by platform + normalized title, not by rating, so a future rating
// refresh cannot silently bring them back.
var excludedTitles = map[string][]string{
	"nes": {
		// Action 52 (1991) lists each of its 52 mini-games as a standalone
		// IGDB entry. Only the compilation itself belongs in the catalog.
		"Alfredo and the Fetucini's",
		"Atmos Quake",
		"Beeps and Blips",
		"Billy-Bob",
		"Bits and Pieces",
		"Boss",
		"Bubble Gum Rosie",
		"Cheetahmen",
		"Chill Out",
		"City of Doom",
		"Crazy Shuffle",
		"Critical Bypass",
		"Cry Baby",
		"Dam Busters",
		"Dedant",
		"Evil Empire",
		"Fire Breathers",
		"French Baker",
		"Futebol",
		"Fuzz Power",
		"G-Force Fighters",
		"Hambo's Adventures",
		"Haunted Halls of Wentworth",
		"Illuminator",
		"Jigsaw",
		"Jupiter Scope",
		"Lazer League",
		"Lollipops",
		"Manchester Beat",
		"Mash Man",
		"Megalonia",
		"Meong",
		"Micro Mike",
		"Ninja Assault",
		"Non Human",
		"Ooze",
		"Operation Full Moon",
		"Robbie and the Robots",
		"Rocket Jockey",
		"Sharks",
		"Shooting Gallery",
		"Silver Sword",
		"Slashers",
		"Sombreros",
		"Space Dreams",
		"Spread Fire",
		"Star Evil",
		"Storm Over the Desert",
		"They Came...",
		"Thrusters",
		"Time Warp Tickers",
		"Underground",
	},
	"snes": {
		// Kirby Super Star lists its sub-games as standalone entries.
		"Dyna Blade",
		"Gourmet Race",
		"Megaton Punch",
		"Milky Way Wishes",
		"Revenge of Meta Knight",
		"Samurai Kirby",
		"The Arena",
		"The Great Cave Offensive",
		// Promotional and competition cartridges, not retail releases.
		"Nintendo Campus Challenge 1992",
		"Donkey Kong Country: Competition Cartridge",
	},
	"gb": {
		// Non-game software packaged as Game Boy cartridges.
		"King James Bible for Use on Game Boy",
		"Personal Organizer and Phone Book",
		"Info Genius Frommer's Travel Guide",
	},
	"gba": {
		"Pocket Professor: Kwik Notes Volume 1",
	},
	"gamecube": {
		// A demo disc and a retail re-release of a game already in the catalog.
		"Welcome to GameCube World!!",
		"Resident Evil 4: GameStop Special Edition",
	},
}

var excludedCatalogEntries = func() map[string]struct{} {
	entries := make(map[string]struct{})
	for platform, titles := range excludedTitles {
		for _, title := range titles {
			entries[platform+"\x00"+catalogKey(title)] = struct{}{}
		}
	}
	return entries
}()

func isExcludedGame(g Game) bool {
	_, ok := excludedCatalogEntries[g.Platform+"\x00"+catalogKey(g.Name)]
	return ok
}

// filterExcludedGames removes excluded artifacts in place and reports how many
// were dropped.
func filterExcludedGames(games []Game) ([]Game, int) {
	kept := games[:0]
	removed := 0
	for _, game := range games {
		if isExcludedGame(game) {
			removed++
			continue
		}
		kept = append(kept, game)
	}
	return kept, removed
}
