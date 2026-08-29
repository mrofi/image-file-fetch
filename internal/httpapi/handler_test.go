package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mrofi/image-file-fetch/internal/extract"
)

func testLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}

func TestHandlerRejectsNonPost(t *testing.T) {
	h := NewDownloadHandler(func(ctx context.Context, req extract.Request, onMatch extract.MatchFunc) error {
		t.Fatal("fetch should not be called")
		return nil
	}, testLogger())

	req := httptest.NewRequest(http.MethodGet, "/download", nil)
	rr := httptest.NewRecorder()
	h(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandlerRejectsInvalidJSON(t *testing.T) {
	h := NewDownloadHandler(func(ctx context.Context, req extract.Request, onMatch extract.MatchFunc) error {
		t.Fatal("fetch should not be called")
		return nil
	}, testLogger())

	req := httptest.NewRequest(http.MethodPost, "/download", strings.NewReader("not json"))
	rr := httptest.NewRecorder()
	h(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandlerMapsStatusErrorFromFetch(t *testing.T) {
	h := NewDownloadHandler(func(ctx context.Context, req extract.Request, onMatch extract.MatchFunc) error {
		return &extract.StatusError{Status: http.StatusNotFound, Msg: "file not found in image"}
	}, testLogger())

	body, _ := json.Marshal(map[string]string{"image": "alpine:latest", "path": "etc/missing"})
	req := httptest.NewRequest(http.MethodPost, "/download", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	h(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
	var payload map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if payload["error"] != "file not found in image" {
		t.Errorf("error message = %q, want %q", payload["error"], "file not found in image")
	}
}

func TestHandlerStreamsMatchedFile(t *testing.T) {
	h := NewDownloadHandler(func(ctx context.Context, req extract.Request, onMatch extract.MatchFunc) error {
		dst, err := onMatch("etc/os-release", 5)
		if err != nil {
			return err
		}
		_, err = dst.Write([]byte("hello"))
		return err
	}, testLogger())

	body, _ := json.Marshal(map[string]string{"image": "alpine:latest", "path": "etc/os-release"})
	req := httptest.NewRequest(http.MethodPost, "/download", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if rr.Body.String() != "hello" {
		t.Errorf("body = %q, want %q", rr.Body.String(), "hello")
	}
	if got := rr.Header().Get("Content-Disposition"); got != `attachment; filename="os-release"` {
		t.Errorf("Content-Disposition = %q, want %q", got, `attachment; filename="os-release"`)
	}
}
