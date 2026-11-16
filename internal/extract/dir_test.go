package extract

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"testing"
)

// readTar returns the entries of a tar as name -> content, with "-> target"
// for links and "<dir>" for directories.
func readTar(t *testing.T, data []byte) map[string]string {
	t.Helper()
	out := map[string]string{}
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("reading result tar: %v", err)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			out[hdr.Name] = "<dir>"
		case tar.TypeSymlink:
			out[hdr.Name] = "-> " + hdr.Linkname
		case tar.TypeLink:
			out[hdr.Name] = "=> " + hdr.Linkname
		default:
			body, _ := io.ReadAll(tr)
			out[hdr.Name] = string(body)
		}
	}
}

func TestScanDirWritesSubtreeWithRelativeNames(t *testing.T) {
	data := buildTar(t,
		[]tar.Header{
			{Name: "etc/passwd", Typeflag: tar.TypeReg},
			{Name: "app/", Typeflag: tar.TypeDir},
			{Name: "app/index.php", Typeflag: tar.TypeReg, Mode: 0o644},
			{Name: "./app/vendor/", Typeflag: tar.TypeDir},
			{Name: "/app/vendor/lib.php", Typeflag: tar.TypeReg},
			{Name: "app/bin", Typeflag: tar.TypeSymlink, Linkname: "vendor/lib.php"},
			{Name: "app/abs-inside", Typeflag: tar.TypeSymlink, Linkname: "/app/vendor/lib.php"},
			{Name: "app/vendor/up", Typeflag: tar.TypeSymlink, Linkname: "/app/index.php"},
			{Name: "app/abs-outside", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
			{Name: "app/hard", Typeflag: tar.TypeLink, Linkname: "app/index.php"},
			{Name: "app/hard-outside", Typeflag: tar.TypeLink, Linkname: "etc/passwd"},
			{Name: "app/dev", Typeflag: tar.TypeChar},
			{Name: "application/not-inside", Typeflag: tar.TypeReg},
		},
		[]string{"root", "", "<?php", "", "lib", "", "", "", "", "", "", "", "other"},
	)

	var out bytes.Buffer
	var gotName string
	var gotSize int64
	found, pending, err := scanDir(bytes.NewReader(data), "app", newVisited("app"), func(name string, size int64) (io.Writer, error) {
		gotName, gotSize = name, size
		return &out, nil
	})
	if err != nil || !found || pending != "" {
		t.Fatalf("scanDir = (%v, %q, %v), want (true, \"\", nil)", found, pending, err)
	}
	if gotName != "app.tar" || gotSize != -1 {
		t.Errorf("onMatch(%q, %d), want (app.tar, -1)", gotName, gotSize)
	}
	want := map[string]string{
		"index.php":      "<?php",
		"vendor/":        "<dir>",
		"vendor/lib.php": "lib",
		"bin":            "-> vendor/lib.php",
		"abs-inside":     "-> vendor/lib.php",
		"vendor/up":      "-> ../index.php",
		"abs-outside":    "-> /etc/passwd",
		"hard":           "=> index.php",
	}
	got := readTar(t, out.Bytes())
	if len(got) != len(want) {
		t.Errorf("entries = %v, want %v", got, want)
	}
	for name, body := range want {
		if got[name] != body {
			t.Errorf("entry %q = %q, want %q", name, got[name], body)
		}
	}
}

func TestScanDirRootTakesEverything(t *testing.T) {
	data := buildTar(t,
		[]tar.Header{{Name: "a", Typeflag: tar.TypeReg}, {Name: "d/b", Typeflag: tar.TypeReg}},
		[]string{"1", "2"},
	)
	var out bytes.Buffer
	found, _, err := scanDir(bytes.NewReader(data), normalizePath("/"), newVisited("."), func(name string, size int64) (io.Writer, error) {
		if name != "root.tar" {
			t.Errorf("onMatch name = %q, want root.tar", name)
		}
		return &out, nil
	})
	if err != nil || !found {
		t.Fatalf("scanDir = (%v, %v)", found, err)
	}
	if got := readTar(t, out.Bytes()); got["a"] != "1" || got["d/b"] != "2" {
		t.Errorf("entries = %v", got)
	}
}

func TestScanDirEmptyDirectoryGivesEmptyArchive(t *testing.T) {
	data := buildTar(t, []tar.Header{{Name: "run/", Typeflag: tar.TypeDir}}, []string{""})
	var out bytes.Buffer
	found, _, err := scanDir(bytes.NewReader(data), "run", newVisited("run"), func(string, int64) (io.Writer, error) { return &out, nil })
	if err != nil || !found {
		t.Fatalf("scanDir = (%v, %v), want found", found, err)
	}
	if got := readTar(t, out.Bytes()); len(got) != 0 {
		t.Errorf("entries = %v, want none", got)
	}
}

func TestScanDirNotFoundSymlinkAndFile(t *testing.T) {
	data := buildTar(t,
		[]tar.Header{
			{Name: "srv/www", Typeflag: tar.TypeSymlink, Linkname: "/opt/site"},
			{Name: "opt/site/index.html", Typeflag: tar.TypeReg},
			{Name: "etc/hostname", Typeflag: tar.TypeReg},
			{Name: "loop", Typeflag: tar.TypeSymlink, Linkname: "loop"},
		},
		[]string{"", "hi", "h", ""},
	)
	called := func(string, int64) (io.Writer, error) {
		t.Error("onMatch called, want no output")
		return io.Discard, nil
	}

	if found, pending, err := scanDir(bytes.NewReader(data), "nope", newVisited("nope"), called); found || pending != "nope" || err != nil {
		t.Errorf("missing dir: (%v, %q, %v), want (false, nope, nil)", found, pending, err)
	}
	// A symlink at the path is reported so the caller scans again for its target.
	if found, pending, err := scanDir(bytes.NewReader(data), "srv/www", newVisited("srv/www"), called); found || pending != "opt/site" || err != nil {
		t.Errorf("symlinked dir: (%v, %q, %v), want (false, opt/site, nil)", found, pending, err)
	}
	var statusErr *StatusError
	if _, _, err := scanDir(bytes.NewReader(data), "etc/hostname", newVisited("etc/hostname"), called); !errors.As(err, &statusErr) || statusErr.Status != 400 {
		t.Errorf("file path: err = %v, want a 400", err)
	}
	if _, _, err := scanDir(bytes.NewReader(data), "loop", newVisited("loop"), called); !errors.As(err, &statusErr) || statusErr.Status != 502 {
		t.Errorf("symlink loop: err = %v, want a 502", err)
	}
}

func TestRequestValidateArchive(t *testing.T) {
	if err := (Request{Image: "a", Path: "/b", Archive: ArchiveTar}).Validate(); err != nil {
		t.Errorf("archive tar: %v", err)
	}
	var statusErr *StatusError
	if err := (Request{Image: "a", Path: "/b", Archive: "zip"}).Validate(); !errors.As(err, &statusErr) || statusErr.Status != 400 {
		t.Errorf("archive zip: err = %v, want a 400", err)
	}
}
