package extract

import "testing"

func TestNormalizePath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/etc/foo", "etc/foo"},
		{"./etc/foo", "etc/foo"},
		{"etc/foo", "etc/foo"},
		{"etc/foo/", "etc/foo"},
		{"/etc/./foo", "etc/foo"},
	}
	for _, c := range cases {
		if got := normalizePath(c.in); got != c.want {
			t.Errorf("normalizePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
