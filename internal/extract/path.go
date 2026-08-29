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
