package main

import (
	"log"
	"net/http"
	"os"

	"github.com/mrofi/image-file-fetch/internal/cli"
	"github.com/mrofi/image-file-fetch/internal/extract"
	"github.com/mrofi/image-file-fetch/internal/httpapi"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "fetch" {
		os.Exit(cli.Run(os.Args[2:], extract.Fetch, os.Stdout, os.Stderr))
	}

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	logger := log.New(os.Stderr, "", log.LstdFlags)

	mux := http.NewServeMux()
	mux.HandleFunc("/download", httpapi.NewDownloadHandler(extract.Fetch, logger))

	logger.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		logger.Fatal(err)
	}
}
