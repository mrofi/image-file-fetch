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

func TestFetchDirectoryAsTar(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in -short mode")
	}

	// /etc/apk in alpine:latest holds the repositories file and a keys
	// directory.
	var out bytes.Buffer
	var gotSize int64
	err := Fetch(context.Background(), Request{
		Image:   "alpine:latest",
		Path:    "/etc/apk",
		Archive: ArchiveTar,
	}, func(name string, size int64) (io.Writer, error) {
		gotSize = size
		return &out, nil
	})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if gotSize != -1 {
		t.Errorf("size = %d, want -1 for an archive", gotSize)
	}
	entries := readTar(t, out.Bytes())
	if !bytes.Contains([]byte(entries["repositories"]), []byte("alpine")) {
		t.Errorf("repositories = %q, want it to mention alpine (entries: %d)", entries["repositories"], len(entries))
	}
	if entries["keys/"] != "<dir>" {
		t.Errorf("keys/ = %q, want a directory entry", entries["keys/"])
	}
	for name := range entries {
		if name == "" || name[0] == '/' || bytes.HasPrefix([]byte(name), []byte("etc/")) {
			t.Errorf("entry %q is not relative to the requested directory", name)
		}
	}
}

func TestFetchDirectoryErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in -short mode")
	}
	for path, want := range map[string]int{"/no/such/dir": http.StatusNotFound, "/etc/alpine-release": http.StatusBadRequest} {
		err := Fetch(context.Background(), Request{Image: "alpine:latest", Path: path, Archive: ArchiveTar},
			func(string, int64) (io.Writer, error) {
				t.Errorf("%s: onMatch called, want an error first", path)
				return io.Discard, nil
			})
		statusErr, ok := err.(*StatusError)
		if !ok || statusErr.Status != want {
			t.Errorf("%s: err = %v, want status %d", path, err, want)
		}
	}
}
