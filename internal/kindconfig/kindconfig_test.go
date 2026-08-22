package kindconfig_test

import (
	"testing"

	"numa-perfman/internal/kindconfig"
)

func TestParseKindVersion(t *testing.T) {
	got, err := kindconfig.ParseKindVersion("kind v0.32.0 go1.24.6 linux/amd64\n")
	if err != nil {
		t.Fatal(err)
	}
	if got != "0.32.0" {
		t.Fatalf("got %q", got)
	}
}

func TestVersionAtLeast(t *testing.T) {
	cases := []struct {
		version, min string
		want         bool
	}{
		{"0.32.0", "0.32.0", true},
		{"0.33.0", "0.32.0", true},
		{"0.31.0", "0.32.0", false},
		{"0.32.1", "0.32.0", true},
	}
	for _, tc := range cases {
		if got := kindconfig.VersionAtLeast(tc.version, tc.min); got != tc.want {
			t.Fatalf("VersionAtLeast(%q, %q) = %v, want %v", tc.version, tc.min, got, tc.want)
		}
	}
}
