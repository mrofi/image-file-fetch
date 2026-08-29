# image-file-fetch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Go webserver that, given a container image reference and an exact path inside it, pulls the image directly from its registry, flattens and streams its layers, follows any symlinks along the way, and streams the matching file back over HTTP — with zero disk writes and zero persisted state.

**Architecture:** `internal/extract` does the registry pull + tar-stream scanning + symlink resolution and is fully unit-testable without a network mock (the tar-scanning core takes an `io.Reader`, not a registry client). `internal/httpapi` is a thin HTTP adapter around it, testable with a fake in place of the real `extract.Fetch`. `main.go` wires the two together.

**Tech Stack:** Go 1.25, standard library `net/http`/`archive/tar`, `github.com/google/go-containerregistry` v0.22.0 for registry access.

**Spec:** `docs/superpowers/specs/2026-08-29-image-file-fetch-design.md`

## Global Constraints

- Module path: `github.com/mrofi/image-file-fetch`.
- Go version: `go 1.25.0` in `go.mod` (a hard floor — `go-containerregistry` v0.22.0 requires it; the `go` toolchain auto-downloads a matching release if the locally installed one is older).
- `github.com/google/go-containerregistry` pinned to `v0.22.0`.
- No file, layer, or extracted content is ever written to local disk — everything streams directly from the registry pull into the HTTP response.
- No file-listing endpoint exists or should be added — callers must know the exact in-image path; an incorrect path is a 404, not a suggestion.
- Registry credentials (`username`/`password`) travel only in a single request's JSON body — never an env var, never logged, never persisted, never reused across requests.
- Symlinks are resolved transparently (see spec's "Symlink resolution" section) — this is normal, expected behavior, not an edge case to special-case away.
- Deployed as a single instance (no autoscaling) — this is why there is intentionally no session-affinity or shared-storage mechanism anywhere in this plan.

Every task's requirements implicitly include the above.

---

### Task 1: Project scaffold + core request/error types

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `LICENSE`
- Create: `README.md`
- Create: `internal/extract/errors.go`
- Create: `internal/extract/request.go`
- Create: `internal/extract/path.go`
- Test: `internal/extract/errors_test.go`
- Test: `internal/extract/request_test.go`
- Test: `internal/extract/path_test.go`

**Interfaces:**
- Produces: `extract.StatusError{Status int, Msg string}` (implements `error`) — every later task's errors are this type.
- Produces: `extract.Request{Image, Path, Username, Password string}` with method `(r Request) Validate() error`.
- Produces: unexported `normalizePath(p string) string` — strips a leading `./` or `/` and `path.Clean`s the result, used by every path comparison in this package.

- [ ] **Step 1: Create the project scaffold files**

`go.mod`:

```
module github.com/mrofi/image-file-fetch

go 1.25.0
```

`.gitignore`:

```
/image-file-fetch
*.test
```

`LICENSE` (MIT, matching the existing `baseimage` repo's license):

```
MIT License

Copyright (c) 2026 mrofi

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

`README.md`:

```markdown
# image-file-fetch

A small Go webserver that extracts one exact file from a container
image and streams it back over HTTP — no Docker daemon, no image
listing, no disk writes. It pulls the image directly from its
registry, flattens its layers in a stream, and copies the matching
file straight into the HTTP response. Symlinks (e.g. `/etc/os-release`
in most base images) are followed transparently.

## Usage

Run the server:

    go run .
    # or: ADDR=:9090 go run .

Download a file:

    curl -X POST http://localhost:8080/download \
      -H 'Content-Type: application/json' \
      -d '{"image": "alpine:latest", "path": "/etc/os-release"}' \
      -o os-release

### Private images

Pass registry credentials in the request body — they are used only
for that single request, never stored:

    curl -X POST http://localhost:8080/download \
      -H 'Content-Type: application/json' \
      -d '{"image": "registry.example.com/repo:tag", "path": "/app/config.yaml", "username": "user", "password": "token"}' \
      -o config.yaml

## Running as a container

    docker build -t image-file-fetch .
    docker run --rm -p 8080:8080 image-file-fetch

## Security

Registry credentials travel in the request body — always run this
behind TLS in any real deployment, never expose it over plain HTTP
outside local development.

## Design

See [docs/superpowers/specs/2026-08-29-image-file-fetch-design.md](docs/superpowers/specs/2026-08-29-image-file-fetch-design.md).

## License

MIT
```

- [ ] **Step 2: Verify the scaffold is valid**

Run: `go vet ./...`
Expected: no output, exit code 0 (no `.go` files exist yet, so there is nothing to vet — this just confirms `go.mod` itself is well-formed).

- [ ] **Step 3: Write the failing tests for the core types**

`internal/extract/errors_test.go`:

```go
package extract

import "testing"

func TestStatusErrorImplementsError(t *testing.T) {
	var err error = &StatusError{Status: 404, Msg: "not found"}
	if err.Error() != "not found" {
		t.Fatalf("Error() = %q, want %q", err.Error(), "not found")
	}
}
```

`internal/extract/request_test.go`:

```go
package extract

import "testing"

func TestRequestValidate(t *testing.T) {
	cases := []struct {
		name    string
		req     Request
		wantErr bool
		wantMsg string
	}{
		{"valid", Request{Image: "alpine:latest", Path: "etc/os-release"}, false, ""},
		{"missing image", Request{Path: "etc/os-release"}, true, "image is required"},
		{"blank image", Request{Image: "   ", Path: "etc/os-release"}, true, "image is required"},
		{"missing path", Request{Image: "alpine:latest"}, true, "path is required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.req.Validate()
			if c.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if c.wantErr {
				se, ok := err.(*StatusError)
				if !ok {
					t.Fatalf("expected *StatusError, got %T", err)
				}
				if se.Status != 400 {
					t.Errorf("Status = %d, want 400", se.Status)
				}
				if se.Msg != c.wantMsg {
					t.Errorf("Msg = %q, want %q", se.Msg, c.wantMsg)
				}
			}
		})
	}
}
```

`internal/extract/path_test.go`:

```go
package extract

import "testing"

func TestNormalizePath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/etc/foo", "etc/foo"},
		{"./etc/foo", "etc/foo"},
		{"etc/foo", "etc/foo"},
		{"etc/foo/", "etc/foo"},
		{"/etc/./foo", "etc/foo"},
	}
	for _, c := range cases {
		if got := normalizePath(c.in); got != c.want {
			t.Errorf("normalizePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test ./internal/extract/... -run 'TestStatusErrorImplementsError|TestRequestValidate|TestNormalizePath' -v`
Expected: FAIL — `undefined: StatusError`, `undefined: Request`, `undefined: normalizePath` (the package doesn't exist yet).

- [ ] **Step 5: Implement the core types**

`internal/extract/errors.go`:

```go
package extract

// StatusError carries the HTTP status code a caller should respond with.
type StatusError struct {
	Status int
	Msg    string
}

func (e *StatusError) Error() string {
	return e.Msg
}
```

`internal/extract/request.go`:

```go
package extract

import "strings"

// Request describes a single file-fetch request.
type Request struct {
	Image    string
	Path     string
	Username string
	Password string
}

// Validate checks that the required fields are present.
func (r Request) Validate() error {
	if strings.TrimSpace(r.Image) == "" {
		return &StatusError{Status: 400, Msg: "image is required"}
	}
	if strings.TrimSpace(r.Path) == "" {
		return &StatusError{Status: 400, Msg: "path is required"}
	}
	return nil
}
```

`internal/extract/path.go`:

```go
package extract

import (
	"path"
	"strings"
)

// normalizePath makes a tar entry name and a user-supplied path directly
// comparable, regardless of leading "./" or "/".
func normalizePath(p string) string {
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	return path.Clean(p)
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/extract/... -v`
Expected: PASS for all three tests.

- [ ] **Step 7: Commit**

```bash
git add go.mod .gitignore LICENSE README.md internal/extract/errors.go internal/extract/errors_test.go internal/extract/request.go internal/extract/request_test.go internal/extract/path.go internal/extract/path_test.go
git commit -m "Add project scaffold and core request/error types"
```

---

### Task 2: Tar-stream scanning with symlink resolution

**Files:**
- Create: `internal/extract/tar.go`
- Test: `internal/extract/tar_test.go`

**Interfaces:**
- Consumes: `StatusError`, `normalizePath` from Task 1.
- Produces: `extract.MatchFunc` (`func(name string, size int64) (io.Writer, error)`) — the callback every later "found a file" path uses.
- Produces: unexported `scanTar(tarStream io.Reader, target string, visited map[string]bool, onMatch MatchFunc) (found bool, pending string, err error)` — the core scanning/symlink-following loop Task 4 (`Fetch`) drives in a retry loop.
- Produces: unexported `resolveSymlink(symlinkPath, linkname string) string`.

- [ ] **Step 1: Write the failing tests**

`internal/extract/tar_test.go`:

```go
package extract

import (
	"archive/tar"
	"bytes"
	"io"
	"testing"
)

func buildTar(t *testing.T, entries []tar.Header, contents []string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for i, hdr := range entries {
		h := hdr
		if h.Typeflag == tar.TypeReg {
			h.Size = int64(len(contents[i]))
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatalf("WriteHeader: %v", err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(contents[i])); err != nil {
				t.Fatalf("Write: %v", err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buf.Bytes()
}

func newVisited(paths ...string) map[string]bool {
	v := make(map[string]bool, len(paths))
	for _, p := range paths {
		v[p] = true
	}
	return v
}

func TestScanTarFindsRegularFile(t *testing.T) {
	data := buildTar(t,
		[]tar.Header{
			{Name: "etc/other", Typeflag: tar.TypeReg},
			{Name: "etc/target", Typeflag: tar.TypeReg},
		},
		[]string{"skip me", "hello world"},
	)

	var out bytes.Buffer
	found, pending, err := scanTar(bytes.NewReader(data), "etc/target", newVisited("etc/target"), func(name string, size int64) (io.Writer, error) {
		if name != "etc/target" {
			t.Errorf("onMatch name = %q, want etc/target", name)
		}
		if size != int64(len("hello world")) {
			t.Errorf("onMatch size = %d, want %d", size, len("hello world"))
		}
		return &out, nil
	})
	if err != nil {
		t.Fatalf("scanTar returned error: %v", err)
	}
	if !found {
		t.Fatalf("found = false, pending = %q, want found = true", pending)
	}
	if out.String() != "hello world" {
		t.Errorf("copied content = %q, want %q", out.String(), "hello world")
	}
}

func TestScanTarNotFound(t *testing.T) {
	data := buildTar(t,
		[]tar.Header{{Name: "etc/other", Typeflag: tar.TypeReg}},
		[]string{"skip me"},
	)

	found, pending, err := scanTar(bytes.NewReader(data), "etc/missing", newVisited("etc/missing"), func(name string, size int64) (io.Writer, error) {
		t.Fatal("onMatch should not be called")
		return nil, nil
	})
	if err != nil {
		t.Fatalf("scanTar returned error: %v", err)
	}
	if found {
		t.Fatal("found = true, want false")
	}
	if pending != "etc/missing" {
		t.Errorf("pending = %q, want %q (unchanged, meaning genuinely absent)", pending, "etc/missing")
	}
}

func TestScanTarDirectoryMatchIsNotFound(t *testing.T) {
	data := buildTar(t,
		[]tar.Header{{Name: "etc/adir", Typeflag: tar.TypeDir}},
		[]string{""},
	)

	_, _, err := scanTar(bytes.NewReader(data), "etc/adir", newVisited("etc/adir"), func(name string, size int64) (io.Writer, error) {
		t.Fatal("onMatch should not be called for a directory")
		return nil, nil
	})
	se, ok := err.(*StatusError)
	if !ok {
		t.Fatalf("expected *StatusError, got %T (%v)", err, err)
	}
	if se.Status != 404 {
		t.Errorf("Status = %d, want 404", se.Status)
	}
}

func TestScanTarCorruptStream(t *testing.T) {
	_, _, err := scanTar(bytes.NewReader([]byte("not a tar file")), "etc/target", newVisited("etc/target"), func(name string, size int64) (io.Writer, error) {
		t.Fatal("onMatch should not be called")
		return nil, nil
	})
	se, ok := err.(*StatusError)
	if !ok {
		t.Fatalf("expected *StatusError, got %T (%v)", err, err)
	}
	if se.Status != 502 {
		t.Errorf("Status = %d, want 502", se.Status)
	}
}

func TestScanTarFollowsSymlinkForwardInSamePass(t *testing.T) {
	data := buildTar(t,
		[]tar.Header{
			{Name: "etc/os-release", Typeflag: tar.TypeSymlink, Linkname: "../usr/lib/os-release"},
			{Name: "usr/lib/os-release", Typeflag: tar.TypeReg},
		},
		[]string{"", "NAME=test"},
	)

	var out bytes.Buffer
	found, _, err := scanTar(bytes.NewReader(data), "etc/os-release", newVisited("etc/os-release"), func(name string, size int64) (io.Writer, error) {
		if name != "usr/lib/os-release" {
			t.Errorf("onMatch name = %q, want usr/lib/os-release", name)
		}
		return &out, nil
	})
	if err != nil {
		t.Fatalf("scanTar returned error: %v", err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	if out.String() != "NAME=test" {
		t.Errorf("copied content = %q, want %q", out.String(), "NAME=test")
	}
}

func TestScanTarSymlinkTargetEarlierInStreamNeedsRestart(t *testing.T) {
	// The regular file appears BEFORE the symlink in tar order, so a
	// single forward pass can't reach it once redirected.
	data := buildTar(t,
		[]tar.Header{
			{Name: "usr/lib/os-release", Typeflag: tar.TypeReg},
			{Name: "etc/os-release", Typeflag: tar.TypeSymlink, Linkname: "../usr/lib/os-release"},
		},
		[]string{"NAME=test", ""},
	)

	found, pending, err := scanTar(bytes.NewReader(data), "etc/os-release", newVisited("etc/os-release"), func(name string, size int64) (io.Writer, error) {
		t.Fatal("onMatch should not be called on the first pass")
		return nil, nil
	})
	if err != nil {
		t.Fatalf("scanTar returned error: %v", err)
	}
	if found {
		t.Fatal("found = true, want false (needs a restart)")
	}
	if pending != "usr/lib/os-release" {
		t.Errorf("pending = %q, want %q", pending, "usr/lib/os-release")
	}
}

func TestScanTarSymlinkCycleIsRejected(t *testing.T) {
	data := buildTar(t,
		[]tar.Header{
			{Name: "a", Typeflag: tar.TypeSymlink, Linkname: "b"},
			{Name: "b", Typeflag: tar.TypeSymlink, Linkname: "a"},
		},
		[]string{"", ""},
	)

	// Simulate having already redirected once from "a" to "b" in a prior
	// pass, so "a" is already visited when "b" tries to redirect back.
	_, _, err := scanTar(bytes.NewReader(data), "b", newVisited("a", "b"), func(name string, size int64) (io.Writer, error) {
		t.Fatal("onMatch should not be called")
		return nil, nil
	})
	se, ok := err.(*StatusError)
	if !ok {
		t.Fatalf("expected *StatusError, got %T (%v)", err, err)
	}
	if se.Status != 502 {
		t.Errorf("Status = %d, want 502", se.Status)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/extract/... -run TestScanTar -v`
Expected: FAIL — `undefined: scanTar`.

- [ ] **Step 3: Implement `scanTar`, `MatchFunc`, and `resolveSymlink`**

`internal/extract/tar.go`:

```go
package extract

import (
	"archive/tar"
	"fmt"
	"io"
	"path"
)

// MatchFunc is called once, when the requested path is found as a regular
// file. It must return the writer the file's contents should be streamed
// into. Returning an error aborts the copy.
type MatchFunc func(name string, size int64) (io.Writer, error)

// resolveSymlink computes the normalized path a symlink at symlinkPath
// pointing to linkname resolves to, honoring both absolute and
// directory-relative link targets.
func resolveSymlink(symlinkPath, linkname string) string {
	if path.IsAbs(linkname) {
		return normalizePath(linkname)
	}
	return normalizePath(path.Join(path.Dir(symlinkPath), linkname))
}

// scanTar reads tarStream once, start to end, looking for target (already
// normalized). If it finds a symlink at the current target, it keeps
// scanning forward in the same pass for the resolved target — most images
// list a symlink's destination in the same layer, so this typically
// resolves without another pull.
//
// visited must contain every path already attempted across this and any
// prior pass; scanTar records each new target it tries and fails fast with
// a 502 if a symlink chain would revisit one (a cycle).
//
// Return values:
//   - (true, "", nil): onMatch was called and its writer was filled.
//   - (false, "", err): a definitive failure (not a regular file, corrupt
//     tar, onMatch/copy error, or symlink cycle).
//   - (false, pending, nil): the stream ended while still looking for
//     pending. If pending == target, that path never matched anything in
//     this full pass and does not exist in the image at all. If pending
//     != target, at least one symlink redirect happened but the final
//     target's entry lies earlier in the tar than the point the redirect
//     was discovered — the caller should re-pull and scan again for
//     pending specifically.
func scanTar(tarStream io.Reader, target string, visited map[string]bool, onMatch MatchFunc) (bool, string, error) {
	current := target
	tr := tar.NewReader(tarStream)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return false, current, nil
		}
		if err != nil {
			return false, "", &StatusError{Status: 502, Msg: fmt.Sprintf("error reading image filesystem: %v", err)}
		}
		if normalizePath(hdr.Name) != current {
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeReg:
			dst, err := onMatch(hdr.Name, hdr.Size)
			if err != nil {
				return false, "", err
			}
			if _, err := io.Copy(dst, tr); err != nil {
				return false, "", &StatusError{Status: 502, Msg: fmt.Sprintf("error streaming file: %v", err)}
			}
			return true, "", nil
		case tar.TypeSymlink:
			next := resolveSymlink(current, hdr.Linkname)
			if visited[next] {
				return false, "", &StatusError{Status: 502, Msg: fmt.Sprintf("symlink loop resolving %q", target)}
			}
			visited[next] = true
			current = next
		default:
			return false, "", &StatusError{Status: 404, Msg: "path exists but is not a regular file"}
		}
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/extract/... -v`
Expected: PASS for all tests (Task 1's tests plus all `TestScanTar*` tests).

- [ ] **Step 5: Commit**

```bash
git add internal/extract/tar.go internal/extract/tar_test.go
git commit -m "Add tar-stream scanning with symlink resolution"
```

---

### Task 3: Registry error mapping

**Files:**
- Create: `internal/extract/remote_error.go`
- Test: `internal/extract/remote_error_test.go`
- Modify: `go.mod`, `go.sum` (adds the `go-containerregistry` dependency)

**Interfaces:**
- Consumes: `StatusError` from Task 1.
- Produces: unexported `mapRemoteError(err error) error` — Task 4 (`Fetch`) calls this on every `remote.Image` failure.

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/google/go-containerregistry@v0.22.0`
Expected: `go.mod` gains a `require github.com/google/go-containerregistry v0.22.0` line.

- [ ] **Step 2: Write the failing test**

`internal/extract/remote_error_test.go`:

```go
package extract

import (
	"errors"
	"net/http"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

func TestMapRemoteError(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"unauthorized", &transport.Error{StatusCode: http.StatusUnauthorized}, http.StatusUnauthorized},
		{"forbidden", &transport.Error{StatusCode: http.StatusForbidden}, http.StatusForbidden},
		{"not found", &transport.Error{StatusCode: http.StatusNotFound}, http.StatusNotFound},
		{"other transport status", &transport.Error{StatusCode: http.StatusInternalServerError}, http.StatusBadGateway},
		{"unrelated error", errors.New("boom"), http.StatusBadGateway},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := mapRemoteError(c.err)
			se, ok := err.(*StatusError)
			if !ok {
				t.Fatalf("expected *StatusError, got %T", err)
			}
			if se.Status != c.wantStatus {
				t.Errorf("Status = %d, want %d", se.Status, c.wantStatus)
			}
		})
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/extract/... -run TestMapRemoteError -v`
Expected: FAIL — `undefined: mapRemoteError`.

- [ ] **Step 4: Implement `mapRemoteError`**

`internal/extract/remote_error.go`:

```go
package extract

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

// mapRemoteError translates an error from the registry client into a
// StatusError carrying the HTTP status this service should respond with.
func mapRemoteError(err error) error {
	var terr *transport.Error
	if errors.As(err, &terr) {
		switch terr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return &StatusError{Status: terr.StatusCode, Msg: fmt.Sprintf("registry auth failed: %v", err)}
		case http.StatusNotFound:
			return &StatusError{Status: http.StatusNotFound, Msg: fmt.Sprintf("image not found: %v", err)}
		}
	}
	return &StatusError{Status: http.StatusBadGateway, Msg: fmt.Sprintf("registry error: %v", err)}
}
```

- [ ] **Step 5: Tidy modules and run the test to verify it passes**

Run: `go mod tidy && go test ./internal/extract/... -v`
Expected: `go.sum` is created/updated with the full dependency tree; all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/extract/remote_error.go internal/extract/remote_error_test.go
git commit -m "Add registry error status mapping"
```

---

### Task 4: Fetch — wiring the registry pull to the tar scanner

**Files:**
- Create: `internal/extract/extract.go`
- Test: `internal/extract/extract_integration_test.go`

**Interfaces:**
- Consumes: `Request.Validate()`, `MatchFunc`, `scanTar`, `mapRemoteError`, `normalizePath` from Tasks 1–3.
- Produces: `extract.Fetch(ctx context.Context, req Request, onMatch MatchFunc) error` — the only exported entry point of this package; Task 6 (`httpapi`) and `main.go` depend on this exact signature.

- [ ] **Step 1: Write the failing integration tests**

These hit the real, public `alpine:latest` image over the network — no mocking, since `go-containerregistry`'s own test suite already covers registry-protocol correctness and a fake here would just test the fake.

`internal/extract/extract_integration_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/extract/... -run TestFetch -v`
Expected: FAIL — `undefined: Fetch`.

- [ ] **Step 3: Implement `Fetch`**

`internal/extract/extract.go`:

```go
package extract

import (
	"context"
	"fmt"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// maxSymlinkAttempts bounds how many times Fetch will re-pull the image to
// keep chasing a symlink chain, so a pathological or malicious image can't
// force unbounded re-pulls.
const maxSymlinkAttempts = 10

// Fetch pulls the image described by req, scans its flattened filesystem
// for req.Path (following symlinks as needed), and streams the matching
// file into whatever writer onMatch returns. onMatch is called at most
// once, only when a matching regular file is found.
func Fetch(ctx context.Context, req Request, onMatch MatchFunc) error {
	if err := req.Validate(); err != nil {
		return err
	}

	ref, err := name.ParseReference(req.Image)
	if err != nil {
		return &StatusError{Status: 400, Msg: fmt.Sprintf("invalid image reference: %v", err)}
	}

	var auth authn.Authenticator = authn.Anonymous
	if req.Username != "" && req.Password != "" {
		auth = &authn.Basic{Username: req.Username, Password: req.Password}
	}

	target := normalizePath(req.Path)
	visited := map[string]bool{target: true}

	for attempt := 0; ; attempt++ {
		if attempt >= maxSymlinkAttempts {
			return &StatusError{Status: 502, Msg: fmt.Sprintf("too many symlink redirects resolving %q", req.Path)}
		}

		img, err := remote.Image(ref, remote.WithAuth(auth), remote.WithContext(ctx))
		if err != nil {
			return mapRemoteError(err)
		}

		rc := mutate.Extract(img)
		found, pending, err := scanTar(rc, target, visited, onMatch)
		rc.Close()
		if err != nil {
			return err
		}
		if found {
			return nil
		}
		if pending == target {
			return &StatusError{Status: 404, Msg: "file not found in image"}
		}
		target = pending
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/extract/... -v`
Expected: PASS for all tests in the package, including the three new integration tests (these make real network calls to Docker Hub and take a few seconds each).

- [ ] **Step 5: Commit**

```bash
git add internal/extract/extract.go internal/extract/extract_integration_test.go
git commit -m "Add Fetch: pull, flatten, and stream a file from a registry image"
```

---

### Task 5: HTTP handler

**Files:**
- Create: `internal/httpapi/handler.go`
- Test: `internal/httpapi/handler_test.go`

**Interfaces:**
- Consumes: `extract.Request`, `extract.MatchFunc`, `extract.StatusError` from `internal/extract` (Tasks 1–4).
- Produces: `httpapi.FetchFunc` (`func(ctx context.Context, req extract.Request, onMatch extract.MatchFunc) error` — matches `extract.Fetch`'s exact signature, so tests can inject a fake). Produces: `httpapi.NewDownloadHandler(fetch FetchFunc, logger *log.Logger) http.HandlerFunc` — `main.go` (Task 6) wires `extract.Fetch` into this.

- [ ] **Step 1: Write the failing tests**

`internal/httpapi/handler_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/httpapi/... -v`
Expected: FAIL — the `httpapi` package doesn't exist yet (`no Go files in ...` / `undefined: NewDownloadHandler`).

- [ ] **Step 3: Implement the handler**

`internal/httpapi/handler.go`:

```go
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/httpapi/... -v`
Expected: PASS for all four tests.

- [ ] **Step 5: Commit**

```bash
git add internal/httpapi/handler.go internal/httpapi/handler_test.go
git commit -m "Add HTTP handler for POST /download"
```

---

### Task 6: main.go + manual smoke test

**Files:**
- Create: `main.go`

**Interfaces:**
- Consumes: `extract.Fetch` (Task 4), `httpapi.NewDownloadHandler` (Task 5).

- [ ] **Step 1: Write `main.go`**

```go
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
```

- [ ] **Step 2: Build it**

Run: `go build -o image-file-fetch .`
Expected: builds with no errors, produces the `image-file-fetch` binary (already covered by `.gitignore`).

- [ ] **Step 3: Manual smoke test against a real public image**

Run in one terminal: `./image-file-fetch`
Expected: prints `listening on :8080`.

In another terminal:

```bash
curl -s -X POST http://localhost:8080/download \
  -H 'Content-Type: application/json' \
  -d '{"image": "alpine:latest", "path": "/etc/os-release"}' \
  -o /tmp/os-release
cat /tmp/os-release
```

Expected: the file downloads successfully and its content mentions "Alpine Linux" (this exercises the symlink-resolution path against a live registry, end-to-end through the actual HTTP handler).

Also verify the not-found case:

```bash
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://localhost:8080/download \
  -H 'Content-Type: application/json' \
  -d '{"image": "alpine:latest", "path": "/no/such/file"}'
```

Expected: prints `404`.

Stop the server (Ctrl-C in its terminal) once both checks pass.

- [ ] **Step 4: Commit**

```bash
git add main.go
git commit -m "Add main.go wiring the HTTP handler to Fetch"
```

---

### Task 7: Dockerfile

**Files:**
- Create: `Dockerfile`
- Create: `.dockerignore`

**Interfaces:**
- Consumes: the full module built in Tasks 1–6.

- [ ] **Step 1: Write the Dockerfile**

`Dockerfile`:

```dockerfile
FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/image-file-fetch .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/image-file-fetch /image-file-fetch
EXPOSE 8080
ENTRYPOINT ["/image-file-fetch"]
```

`.dockerignore`:

```
.git
docs
*.md
```

- [ ] **Step 2: Build the image**

Run: `docker build -t image-file-fetch .`
Expected: builds successfully (needs network access during the build for `go mod download` and the base image pulls).

- [ ] **Step 3: Smoke test the container**

```bash
docker run --rm -d -p 8080:8080 --name image-file-fetch-smoke image-file-fetch
sleep 1
curl -s -X POST http://localhost:8080/download \
  -H 'Content-Type: application/json' \
  -d '{"image": "alpine:latest", "path": "/etc/os-release"}' \
  -o /tmp/os-release-container
cat /tmp/os-release-container
docker stop image-file-fetch-smoke
```

Expected: the downloaded content mentions "Alpine Linux", same as the non-containerized smoke test in Task 6.

- [ ] **Step 4: Commit**

```bash
git add Dockerfile .dockerignore
git commit -m "Add Dockerfile"
```

---

### Task 8: CI workflow

**Files:**
- Create: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: the module and Dockerfile from Tasks 1–7.

- [ ] **Step 1: Write the workflow**

`.github/workflows/ci.yml` (test gate on every push/PR; build-and-push to GHCR only on pushes to `main` or a `v*` tag, matching the pattern already used by the `baseimage` repo's `publish.yml`):

```yaml
name: CI

on:
  push:
    branches: [main]
    tags: ["v*"]
  pull_request:

permissions:
  contents: read
  packages: write

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: "1.25"

      - name: Vet
        run: go vet ./...

      - name: Test
        run: go test ./...

  build-and-push:
    needs: test
    if: github.event_name == 'push'
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Log in to GHCR
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Extract metadata
        id: meta
        uses: docker/metadata-action@v5
        with:
          images: ghcr.io/${{ github.repository }}
          tags: |
            type=raw,value=latest,enable={{is_default_branch}}
            type=ref,event=tag

      - name: Build and push
        uses: docker/build-push-action@v6
        with:
          context: .
          push: true
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
```

- [ ] **Step 2: Verify locally what CI will run**

Run: `go vet ./... && go test ./...`
Expected: both succeed (this is the exact command sequence the `test` job runs).

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "Add CI workflow: test on every push/PR, publish to GHCR on main/tags"
```

---

### Task 9: Create the GitHub repo and push

**Files:** none (repo administration only).

- [ ] **Step 1: Create the empty GitHub repo**

If the `gh` CLI is available (`gh --version`), run:

```bash
gh repo create mrofi/image-file-fetch --public --source=. --remote=origin
```

If `gh` is not installed (it wasn't in this environment as of this plan being written — confirm with `command -v gh`), create the empty repository manually at https://github.com/new (owner `mrofi`, name `image-file-fetch`, no README/license/gitignore — this repo already has all three), then add the remote:

```bash
git remote add origin git@github.com:mrofi/image-file-fetch.git
```

- [ ] **Step 2: Push**

```bash
git push -u origin main
```

Expected: all commits from Tasks 1–8 (plus the two spec commits already on `main`) land on `github.com/mrofi/image-file-fetch`.

- [ ] **Step 3: Verify**

Run: `git log --oneline -1` and compare against the GitHub web UI's latest commit for the repo, or run `git fetch origin && git log origin/main --oneline -1` and confirm it matches local `main`.
