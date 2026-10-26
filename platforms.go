package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Platform describes one supported IGDB platform.
type Platform struct {
	Slug   string
	Name   string
	Short  string
	IGDBID int
}

// PlatformCount is a facet used by the platform filter chips.
type PlatformCount struct {
	Slug  string `json:"slug"`
	Name  string `json:"name"`
	Short string `json:"short"`
	Count int    `json:"count"`
}

// supportedPlatforms is the fixed platform set for this catalog. The IGDB IDs
// are stable enum values (see the IGDB enums-to-tables migration).
var supportedPlatforms = []Platform{
	{Slug: "nes", Name: "Nintendo Entertainment System", Short: "NES", IGDBID: 18},
	{Slug: "snes", Name: "Super Nintendo Entertainment System", Short: "SNES", IGDBID: 19},
	{Slug: "genesis", Name: "Sega Genesis", Short: "Genesis", IGDBID: 29},
	{Slug: "tg16", Name: "TurboGrafx-16", Short: "TG16", IGDBID: 86},
}

func platformBySlug(slug string) *Platform {
	for i := range supportedPlatforms {
		if supportedPlatforms[i].Slug == slug {
			return &supportedPlatforms[i]
		}
	}
	return nil
}

func platformByIGDBID(id int) *Platform {
	for i := range supportedPlatforms {
		if supportedPlatforms[i].IGDBID == id {
			return &supportedPlatforms[i]
		}
	}
	return nil
}

// supportedPlatformIDs returns the IGDB platform IDs in registry order.
func supportedPlatformIDs() []string {
	ids := make([]string, len(supportedPlatforms))
	for i, p := range supportedPlatforms {
		ids[i] = strconv.Itoa(p.IGDBID)
	}
	return ids
}

// parseGameID parses igdb-<gameID>-<platformSlug>.
func parseGameID(id string) (int, string, bool) {
	if len(id) == 0 || len(id) > 64 {
		return 0, "", false
	}
	parts := strings.Split(id, "-")
	if len(parts) != 3 || parts[0] != "igdb" {
		return 0, "", false
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil || n <= 0 {
		return 0, "", false
	}
	if platformBySlug(parts[2]) == nil {
		return 0, "", false
	}
	return n, parts[2], true
}

func platformCountsSummary(platforms []PlatformCount) string {
	parts := make([]string, 0, len(platforms))
	for _, p := range platforms {
		parts = append(parts, fmt.Sprintf("%s %d", p.Short, p.Count))
	}
	return strings.Join(parts, ", ")
}
