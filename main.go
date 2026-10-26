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
	cacheDir := flag.String("cache", "data", "directory containing catalog-v2.json")
	exportDir := flag.String("export", "dist", "directory to write the static site")
	refresh := flag.Bool("refresh", false, "ignore a fresh catalog cache and fetch from IGDB")
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
		client := newIGDBClient(clientID, clientSecret)
		fetch = client.fetchCatalog
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
