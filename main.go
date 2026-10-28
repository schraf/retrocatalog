package main

import (
	"context"
	"embed"
	"flag"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"syscall"
)

//go:embed web
var webFiles embed.FS

func main() {
	cacheDir := flag.String("cache", "data", "directory containing catalog-v3.json")
	exportDir := flag.String("export", "dist", "directory to write the static site")
	refresh := flag.Bool("refresh", false, "ignore a fresh catalog cache and fetch from IGDB and LaunchBox")
	launchBoxMetadata := flag.String("launchbox-metadata", "", "local LaunchBox Metadata.xml or .zip; downloads the current export when empty")
	flag.Parse()

	if err := loadDotEnv(".env"); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	clientID := os.Getenv("IGDB_CLIENT_ID")
	clientSecret := os.Getenv("IGDB_CLIENT_SECRET")
	var fetch fetchFunc
	if clientID != "" && clientSecret != "" {
		igdbClient := newIGDBClient(clientID, clientSecret)
		launchBox := newLaunchBoxClient(*launchBoxMetadata)
		fetch = func(ctx context.Context) ([]Game, error) {
			games, err := igdbClient.fetchCatalog(ctx)
			if err != nil {
				return nil, err
			}
			entries, err := launchBox.fetch(ctx)
			if err != nil {
				return nil, err
			}
			stats := mergeLaunchBoxRatings(games, entries)
			log.Printf("launchbox: matched %d/%d (exact %d, alias %d, fuzzy %d); ratings: %d LaunchBox, %d IGDB, %d unrated",
				stats.Matched, stats.Games, stats.Exact, stats.Alias, stats.Fuzzy, stats.LaunchBox, stats.IGDB, stats.Unrated)
			return games, nil
		}
	}
	if *refresh && fetch == nil {
		log.Fatal("refresh requires IGDB_CLIENT_ID and IGDB_CLIENT_SECRET")
	}

	catalog, err := loadOrFetch(ctx, *cacheDir, *refresh, fetch, log.Printf)
	if err != nil {
		log.Fatal(err)
	}

	webRoot, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatal(err)
	}
	if err := exportStatic(*exportDir, webRoot, catalog); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s (%d games)", *exportDir, catalog.Total())
}
