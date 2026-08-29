package extract

import (
	"errors"
	"net/http"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

func TestMapRemoteError(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"unauthorized", &transport.Error{StatusCode: http.StatusUnauthorized}, http.StatusUnauthorized},
		{"forbidden", &transport.Error{StatusCode: http.StatusForbidden}, http.StatusForbidden},
		{"not found", &transport.Error{StatusCode: http.StatusNotFound}, http.StatusNotFound},
		{"other transport status", &transport.Error{StatusCode: http.StatusInternalServerError}, http.StatusBadGateway},
		{"unrelated error", errors.New("boom"), http.StatusBadGateway},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := mapRemoteError(c.err)
			se, ok := err.(*StatusError)
			if !ok {
				t.Fatalf("expected *StatusError, got %T", err)
			}
			if se.Status != c.wantStatus {
				t.Errorf("Status = %d, want %d", se.Status, c.wantStatus)
			}
		})
	}
}
