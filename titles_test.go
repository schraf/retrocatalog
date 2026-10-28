package main

import "testing"

func TestCatalogKey(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Super Mario Bros. 3", "super mario bros 3"},
		{"Pokémon Red Version", "pokemon red version"},
		{"Ranma ½", "ranma 1 2"},
		{"720°", "720"},
		{"Kirby's Adventure", "kirby s adventure"},
		{"Dungeons & Dragons", "dungeons and dragons"},
		{"  The   Legend of Zelda  ", "the legend of zelda"},
	}
	for _, test := range tests {
		if got := catalogKey(test.in); got != test.want {
			t.Errorf("catalogKey(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

func TestMatchKey(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Disney's DuckTales", "ducktales"},
		{"Walt Disney's The Jungle Book", "the jungle book"},
		{"Tom Clancy's Ghost Recon 2", "ghost recon 2"},
		{"John Romero's Daikatana", "daikatana"},
		{"Dr. Seuss': The Cat in the Hat", "the cat in the hat"},
		{"Street Fighter Alpha 3 Upper", "street fighter alpha 3"},
		{"Ninja Gaiden II: The Dark Sword of Chaos", "ninja gaiden 2 the dark sword of chaos"},
		{"Wolfenstein 3D", "wolfenstein 3d"},
		{"Wolfenstein 3-D", "wolfenstein 3d"},
		{"PGA European Tour Golf", "pga european tour golf"},
	}
	for _, test := range tests {
		if got := matchKey(test.in); got != test.want {
			t.Errorf("matchKey(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

func TestNameSimilarity(t *testing.T) {
	a := keyTokenSet("terminator 2 judgment day")
	b := keyTokenSet("t2 terminator 2 judgment day")
	if got := nameSimilarity(a, b); got < 0.79 || got > 0.81 {
		t.Fatalf("nameSimilarity = %v, want 0.8", got)
	}
	if got := nameSimilarity(a, keyTokenSet("")); got != 0 {
		t.Fatalf("nameSimilarity with empty set = %v, want 0", got)
	}
}

func TestTokenSubset(t *testing.T) {
	short := keyTokenSet("doom 2")
	long := keyTokenSet("doom 2 hell on earth")
	if !tokenSubset(short, long) {
		t.Fatal("doom 2 should be a subset of doom 2 hell on earth")
	}
	if tokenSubset(long, short) {
		t.Fatal("doom 2 hell on earth should not be a subset of doom 2")
	}
	if tokenSubset(keyTokenSet(""), long) {
		t.Fatal("an empty set should not count as a subset")
	}
}

func TestLaunchBoxAliasIndex(t *testing.T) {
	target, ok := launchBoxAlias("snes", matchKey("Ninja Gaiden II: The Dark Sword of Chaos"))
	if !ok || target != "ninja gaiden trilogy" {
		t.Fatalf("alias = %q, %v; want ninja gaiden trilogy", target, ok)
	}
	if _, ok := launchBoxAlias("nes", matchKey("Ninja Gaiden")); ok {
		t.Fatal("aliases must be platform-specific")
	}
}
