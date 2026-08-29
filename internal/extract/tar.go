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
