package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrofi/image-file-fetch/internal/extract"
)

func TestRunRejectsWrongArgCount(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"alpine:latest"}, func(ctx context.Context, req extract.Request, onMatch extract.MatchFunc) error {
		t.Fatal("fetch should not be called")
		return nil
	}, &stdout, &stderr)

	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestRunWritesToOutputFile(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "os-release.txt")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"-output", outPath, "alpine:latest", "etc/os-release"}, func(ctx context.Context, req extract.Request, onMatch extract.MatchFunc) error {
		if req.Image != "alpine:latest" || req.Path != "etc/os-release" {
			t.Errorf("unexpected request: %+v", req)
		}
		dst, err := onMatch("etc/os-release", 5)
		if err != nil {
			return err
		}
		_, err = dst.Write([]byte("hello"))
		return err
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("file contents = %q, want %q", got, "hello")
	}
}

func TestRunDefaultsOutputToPathBasename(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"alpine:latest", "etc/os-release"}, func(ctx context.Context, req extract.Request, onMatch extract.MatchFunc) error {
		dst, err := onMatch("etc/os-release", 5)
		if err != nil {
			return err
		}
		_, err = dst.Write([]byte("hello"))
		return err
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "os-release")); err != nil {
		t.Errorf("expected default output file: %v", err)
	}
}

func TestRunWritesToStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"-output", "-", "alpine:latest", "etc/os-release"}, func(ctx context.Context, req extract.Request, onMatch extract.MatchFunc) error {
		dst, err := onMatch("etc/os-release", 5)
		if err != nil {
			return err
		}
		_, err = dst.Write([]byte("hello"))
		return err
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	if stdout.String() != "hello" {
		t.Errorf("stdout = %q, want %q", stdout.String(), "hello")
	}
}

func TestRunReportsFetchErrorAndCleansUpFile(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"-output", outPath, "alpine:latest", "etc/missing"}, func(ctx context.Context, req extract.Request, onMatch extract.MatchFunc) error {
		return &extract.StatusError{Status: 404, Msg: "file not found in image"}
	}, &stdout, &stderr)

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "file not found in image") {
		t.Errorf("stderr = %q, want it to contain the error message", stderr.String())
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Errorf("expected output file to be removed after error, stat err = %v", err)
	}
}

func TestRunPassesCredentials(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"-username", "user", "-password", "token", "-output", "-", "registry.example.com/repo:tag", "app/config.yaml"},
		func(ctx context.Context, req extract.Request, onMatch extract.MatchFunc) error {
			if req.Username != "user" || req.Password != "token" {
				t.Errorf("credentials = %+v, want user/token", req)
			}
			dst, err := onMatch("app/config.yaml", 1)
			if err != nil {
				return err
			}
			_, err = io.WriteString(dst, "x")
			return err
		}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
}
