package main

import "testing"

func TestIsUnsupportedIPLocation(t *testing.T) {
	cases := []struct {
		loc  string
		want bool
	}{
		{loc: "CN", want: true},
		{loc: "HK", want: true},
		{loc: "MO", want: true},
		{loc: "TW", want: true},
		{loc: "tw", want: true},
		{loc: " US ", want: false},
		{loc: "", want: false},
	}

	for _, tc := range cases {
		if got := isUnsupportedIPLocation(tc.loc); got != tc.want {
			t.Fatalf("isUnsupportedIPLocation(%q) = %v, want %v", tc.loc, got, tc.want)
		}
	}
}
