package store

import "testing"

func TestValidateEmail(t *testing.T) {
	cases := []struct {
		in    string
		ok    bool
	}{
		{"a@b.co", true},
		{"  user@example.com ", true},
		{"", false},
		{"nope", false},
		{"Name <a@b.co>", false},
		{"a@b", true}, // ParseAddress accepts this
	}
	for _, tc := range cases {
		err := ValidateEmail(tc.in)
		if tc.ok && err != nil {
			t.Errorf("%q: unexpected err %v", tc.in, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%q: expected error", tc.in)
		}
	}
}

func TestNormalizeEmail(t *testing.T) {
	if got := NormalizeEmail("  Foo@Example.COM "); got != "foo@example.com" {
		t.Fatalf("got %q", got)
	}
}
