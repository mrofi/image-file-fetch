package extract

import (
	"archive/tar"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"
)

// ArchiveTar is the Request.Archive value that asks for a directory as a
// tar stream instead of a single file.
const ArchiveTar = "tar"

// relUnder returns name's path relative to dir, and whether name is
// strictly inside dir. Both are normalized; dir "." is the image root.
func relUnder(name, dir string) (string, bool) {
	if dir == "." {
		return name, name != "."
	}
	if !strings.HasPrefix(name, dir+"/") {
		return "", false
	}
	return name[len(dir)+1:], true
}

// relinkInside rewrites an absolute symlink target that points inside dir
// into one relative to the link, so the link still works once the
// directory is unpacked somewhere else. Every other target is returned
// unchanged.
func relinkInside(linkPath, linkname, dir string) string {
	if !path.IsAbs(linkname) {
		return linkname
	}
	resolved := normalizePath(linkname)
	if _, ok := relUnder(resolved, dir); !ok && resolved != dir {
		return linkname
	}
	rel, err := filepath.Rel(path.Dir(linkPath), resolved)
	if err != nil {
		return linkname
	}
	return filepath.ToSlash(rel)
}

// scanDir reads tarStream once and writes every entry under target (a
// normalized directory path) to the writer onMatch returns, as a tar whose
// entry names are relative to target. onMatch is called once, with size -1,
// when the first entry is about to be written.
//
// Regular files, directories and symlinks are kept; a hard link is kept
// when the file it names is inside target too. Absolute symlinks that point
// inside target are made relative. Devices and other special files are
// left out.
//
// Return values mirror scanTar:
//   - (true, "", nil): the archive was written.
//   - (false, "", err): a definitive failure (target is a regular file,
//     corrupt tar, write error, symlink cycle).
//   - (false, pending, nil): nothing was written. If pending == target the
//     directory does not exist in the image; otherwise target is a symlink
//     and the caller should scan again for pending.
func scanDir(tarStream io.Reader, target string, visited map[string]bool, onMatch MatchFunc) (bool, string, error) {
	tr := tar.NewReader(tarStream)
	var tw *tar.Writer
	start := func() error {
		if tw != nil {
			return nil
		}
		name := path.Base(target)
		if target == "." {
			name = "root"
		}
		dst, err := onMatch(name+".tar", -1)
		if err != nil {
			return err
		}
		tw = tar.NewWriter(dst)
		return nil
	}
	writeErr := func(err error) error {
		return &StatusError{Status: 502, Msg: fmt.Sprintf("error streaming directory: %v", err)}
	}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			if tw == nil {
				return false, target, nil
			}
			if err := tw.Close(); err != nil {
				return false, "", writeErr(err)
			}
			return true, "", nil
		}
		if err != nil {
			return false, "", &StatusError{Status: 502, Msg: fmt.Sprintf("error reading image filesystem: %v", err)}
		}
		name := normalizePath(hdr.Name)
		if name == target && target != "." {
			switch hdr.Typeflag {
			case tar.TypeDir:
				// An empty directory still yields a (then empty) archive.
				if err := start(); err != nil {
					return false, "", err
				}
			case tar.TypeSymlink:
				if tw != nil {
					return false, "", &StatusError{Status: 502, Msg: "path is both a directory and a symlink in the image"}
				}
				next := resolveSymlink(target, hdr.Linkname)
				if visited[next] {
					return false, "", &StatusError{Status: 502, Msg: fmt.Sprintf("symlink loop resolving %q", target)}
				}
				visited[next] = true
				return false, next, nil
			default:
				return false, "", &StatusError{Status: 400, Msg: "path is a file, not a directory (leave out archive to fetch it)"}
			}
			continue
		}
		rel, ok := relUnder(name, target)
		if !ok {
			continue
		}
		out := &tar.Header{Name: rel, Typeflag: hdr.Typeflag, Mode: hdr.Mode & 0o7777, ModTime: hdr.ModTime}
		switch hdr.Typeflag {
		case tar.TypeReg:
			out.Size = hdr.Size
		case tar.TypeDir:
			out.Name += "/"
		case tar.TypeSymlink:
			out.Linkname = relinkInside(name, hdr.Linkname, target)
		case tar.TypeLink:
			linked, ok := relUnder(normalizePath(hdr.Linkname), target)
			if !ok {
				continue
			}
			out.Linkname = linked
		default:
			continue
		}
		if err := start(); err != nil {
			return false, "", err
		}
		if err := tw.WriteHeader(out); err != nil {
			return false, "", writeErr(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			if _, err := io.Copy(tw, tr); err != nil {
				return false, "", writeErr(err)
			}
		}
	}
}
