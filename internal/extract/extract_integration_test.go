package extract

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
)

func TestFetchResolvesSymlinkToRegularFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in -short mode")
	}

	// /etc/os-release in alpine:latest is a symlink to
	// /usr/lib/os-release; Fetch must resolve it transparently.
	var out bytes.Buffer
	var gotName string
	var gotSize int64

	err := Fetch(context.Background(), Request{
		Image: "alpine:latest",
		Path:  "/etc/os-release",
	}, func(name string, size int64) (io.Writer, error) {
		gotName = name
		gotSize = size
		return &out, nil
	})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if gotName != "usr/lib/os-release" {
		t.Errorf("matched name = %q, want usr/lib/os-release (the symlink's resolved target)", gotName)
	}
	if gotSize == 0 {
		t.Error("matched size = 0, want > 0")
	}
	if !bytes.Contains(out.Bytes(), []byte("Alpine Linux")) {
		t.Errorf("downloaded content = %q, want it to mention Alpine Linux", out.String())
	}
}

func TestFetchDownloadsRegularFileDirectly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in -short mode")
	}

	var out bytes.Buffer
	err := Fetch(context.Background(), Request{
		Image: "alpine:latest",
		Path:  "usr/lib/os-release",
	}, func(name string, size int64) (io.Writer, error) {
		return &out, nil
	})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("Alpine Linux")) {
		t.Errorf("downloaded content = %q, want it to mention Alpine Linux", out.String())
	}
}

func TestFetchMissingPathReturns404(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in -short mode")
	}

	err := Fetch(context.Background(), Request{
		Image: "alpine:latest",
		Path:  "/this/path/does/not/exist",
	}, func(name string, size int64) (io.Writer, error) {
		t.Fatal("onMatch should not be called")
		return nil, nil
	})
	se, ok := err.(*StatusError)
	if !ok {
		t.Fatalf("expected *StatusError, got %T (%v)", err, err)
	}
	if se.Status != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", se.Status, http.StatusNotFound)
	}
}
