package main

import (
	"strings"
	"testing"
)

func TestSupportedPlatforms(t *testing.T) {
	slugs := make(map[string]bool, len(supportedPlatforms))
	ids := make(map[int]bool, len(supportedPlatforms))

	for _, p := range supportedPlatforms {
		if p.Slug == "" || p.Name == "" || p.Short == "" {
			t.Errorf("platform %#v has an empty field", p)
		}
		if strings.Contains(p.Slug, "-") {
			t.Errorf("platform slug %q must not contain a hyphen", p.Slug)
		}
		if p.IGDBID <= 0 {
			t.Errorf("platform %q has invalid IGDB ID %d", p.Slug, p.IGDBID)
		}
		if p.LastYear <= 0 {
			t.Errorf("platform %q has invalid LastYear %d", p.Slug, p.LastYear)
		}
		if slugs[p.Slug] {
			t.Errorf("duplicate platform slug %q", p.Slug)
		}
		if ids[p.IGDBID] {
			t.Errorf("duplicate IGDB platform ID %d", p.IGDBID)
		}
		if _, slug, ok := parseGameID("igdb-1-" + p.Slug); !ok || slug != p.Slug {
			t.Errorf("parseGameID round trip failed for %q", p.Slug)
		}

		slugs[p.Slug] = true
		ids[p.IGDBID] = true
	}
}

func TestPlatformLastYearCutoffs(t *testing.T) {
	for _, p := range supportedPlatforms {
		t.Run(p.Slug, func(t *testing.T) {
			games := []Game{
				testGame("igdb-1-"+p.Slug, "Last Licensed", p.Slug, p.LastYear),
				testGame("igdb-2-"+p.Slug, "Too Late", p.Slug, p.LastYear+1),
			}

			catalog, err := buildCatalog(games)
			if err != nil {
				t.Fatal(err)
			}
			if catalog.Total() != 1 {
				t.Fatalf("Total() = %d, want 1", catalog.Total())
			}
			if catalog.ByID("igdb-1-"+p.Slug) == nil {
				t.Error("game at LastYear should be kept")
			}
			if catalog.ByID("igdb-2-"+p.Slug) != nil {
				t.Error("game after LastYear should be excluded")
			}
		})
	}
}
