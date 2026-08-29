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
