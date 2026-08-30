// Package cli implements the "fetch" subcommand: a direct, non-HTTP way to
// pull one file out of a container image and write it to a local path or
// stdout.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/mrofi/image-file-fetch/internal/extract"
)

// FetchFunc matches extract.Fetch's signature, so tests can inject a fake
// without hitting a real registry.
type FetchFunc func(ctx context.Context, req extract.Request, onMatch extract.MatchFunc) error

// Run executes the "fetch" subcommand. args excludes the program name and
// the "fetch" subcommand word itself (i.e. os.Args[2:]). It returns the
// process exit code.
func Run(args []string, fetch FetchFunc, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: image-file-fetch fetch [-username U] [-password P] [-output FILE] <image> <path>")
		fs.PrintDefaults()
	}
	username := fs.String("username", "", "registry username (for private images)")
	password := fs.String("password", "", "registry password (for private images)")
	output := fs.String("output", "", "output file path, or - for stdout (default: basename of <path> in the current directory)")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fs.Usage()
		return 2
	}

	req := extract.Request{
		Image:    fs.Arg(0),
		Path:     fs.Arg(1),
		Username: *username,
		Password: *password,
	}

	dest := *output
	if dest == "" {
		dest = path.Base(req.Path)
	}

	var createdFile *os.File
	err := fetch(context.Background(), req, func(name string, size int64) (io.Writer, error) {
		if dest == "-" {
			return stdout, nil
		}
		f, err := os.Create(dest)
		if err != nil {
			return nil, err
		}
		createdFile = f
		return f, nil
	})

	if createdFile != nil {
		createdFile.Close()
	}
	if err != nil {
		if createdFile != nil {
			os.Remove(dest)
		}
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	if dest != "-" {
		fmt.Fprintf(stderr, "wrote %s\n", dest)
	}
	return 0
}
