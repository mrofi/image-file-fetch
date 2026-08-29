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
