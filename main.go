package main

import (
	"log"
	"net/http"
	"os"

	"github.com/mrofi/image-file-fetch/internal/extract"
	"github.com/mrofi/image-file-fetch/internal/httpapi"
)

func main() {
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
