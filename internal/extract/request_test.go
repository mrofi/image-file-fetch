package extract

import "testing"

func TestRequestValidate(t *testing.T) {
	cases := []struct {
		name    string
		req     Request
		wantErr bool
		wantMsg string
	}{
		{"valid", Request{Image: "alpine:latest", Path: "etc/os-release"}, false, ""},
		{"missing image", Request{Path: "etc/os-release"}, true, "image is required"},
		{"blank image", Request{Image: "   ", Path: "etc/os-release"}, true, "image is required"},
		{"missing path", Request{Image: "alpine:latest"}, true, "path is required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.req.Validate()
			if c.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if c.wantErr {
				se, ok := err.(*StatusError)
				if !ok {
					t.Fatalf("expected *StatusError, got %T", err)
				}
				if se.Status != 400 {
					t.Errorf("Status = %d, want 400", se.Status)
				}
				if se.Msg != c.wantMsg {
					t.Errorf("Msg = %q, want %q", se.Msg, c.wantMsg)
				}
			}
		})
	}
}
