package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"path"
	"strconv"

	"github.com/mrofi/image-file-fetch/internal/extract"
)

// FetchFunc matches extract.Fetch's signature, so tests can inject a fake
// without hitting a real registry.
type FetchFunc func(ctx context.Context, req extract.Request, onMatch extract.MatchFunc) error

type downloadRequest struct {
	Image    string `json:"image"`
	Path     string `json:"path"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// NewDownloadHandler returns the handler for POST /download.
func NewDownloadHandler(fetch FetchFunc, logger *log.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		defer r.Body.Close()

		var body downloadRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		req := extract.Request{
			Image:    body.Image,
			Path:     body.Path,
			Username: body.Username,
			Password: body.Password,
		}

		responded := false
		err := fetch(r.Context(), req, func(name string, size int64) (io.Writer, error) {
			responded = true
			base := path.Base(name)
			w.Header().Set("Content-Disposition", "attachment; filename=\""+base+"\"")
			w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
			ct := mime.TypeByExtension(path.Ext(base))
			if ct == "" {
				ct = "application/octet-stream"
			}
			w.Header().Set("Content-Type", ct)
			w.WriteHeader(http.StatusOK)
			return w, nil
		})

		if err == nil {
			return
		}
		if responded {
			logger.Printf("error after response started: %v", err)
			return
		}

		var statusErr *extract.StatusError
		if errors.As(err, &statusErr) {
			writeError(w, statusErr.Status, statusErr.Msg)
			return
		}
		logger.Printf("unexpected error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
