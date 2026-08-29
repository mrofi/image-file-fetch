package extract

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

// mapRemoteError translates an error from the registry client into a
// StatusError carrying the HTTP status this service should respond with.
func mapRemoteError(err error) error {
	var terr *transport.Error
	if errors.As(err, &terr) {
		switch terr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return &StatusError{Status: terr.StatusCode, Msg: fmt.Sprintf("registry auth failed: %v", err)}
		case http.StatusNotFound:
			return &StatusError{Status: http.StatusNotFound, Msg: fmt.Sprintf("image not found: %v", err)}
		}
	}
	return &StatusError{Status: http.StatusBadGateway, Msg: fmt.Sprintf("registry error: %v", err)}
}
