package extract

import "strings"

// Request describes a single fetch: one file, or with Archive set to
// ArchiveTar, one directory as a tar stream.
type Request struct {
	Image    string
	Path     string
	Archive  string
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
	if r.Archive != "" && r.Archive != ArchiveTar {
		return &StatusError{Status: 400, Msg: `archive must be "tar" or left out`}
	}
	return nil
}
