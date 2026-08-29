package extract

import "testing"

func TestStatusErrorImplementsError(t *testing.T) {
	var err error = &StatusError{Status: 404, Msg: "not found"}
	if err.Error() != "not found" {
		t.Fatalf("Error() = %q, want %q", err.Error(), "not found")
	}
}
