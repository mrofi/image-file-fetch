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
