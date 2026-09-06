// Package selfupdate's own test package is used (not selfupdate_test) per
// the task spec, so these tests can be extended later to exercise unexported
// helpers without a second test package.
//
//nolint:testpackage // internal package per task spec envctl-04qa.00bow5.1.1.2
package selfupdate

import "testing"

// normV010 and normRC1 name the normalized forms that both a bare and a
// v-prefixed input map to below. Naming them avoids repeating the string
// literal enough times to trip goconst.
const (
	normV010   = "v0.1.0"
	normRC1    = "v1.2.3-rc.1"
	legacyRC9  = "v0.16.0-rc9"
	legacyRC10 = "v0.16.0-rc10"
	dottedRC9  = "v0.16.0-rc.9"
	dottedRC10 = "v0.16.0-rc.10"
)

func TestNormalizeVersion(t *testing.T) {
	cases := map[string]string{
		"":           "v0.0.0-dev",
		"dev":        "v0.0.0-dev",
		"0.1.0":      normV010,
		"v0.1.0":     normV010,
		"1.2.3-rc.1": normRC1,
		normRC1:      normRC1,
		legacyRC9:    legacyRC9,
		legacyRC10:   legacyRC10,
	}
	for in, want := range cases {
		if got := NormalizeVersion(in); got != want {
			t.Errorf("NormalizeVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		current, latest string
		sign            int
	}{
		{"dev", "v1.0.0", 1},
		{"1.2.3", "v1.2.3", 0},
		{"v1.3.0-rc.1", "v1.3.0", 1},
		{"v2.0.0", "v1.0.0", -1},
		{"v1.3.0-rc.1", "v1.3.0-rc.2", 1},
		{"v1.3.0", "v1.3.0-rc.9", -1},
		{"v0.6.0", "v0.6.1-nightly.20260904", 1},
		{"v0.6.1-nightly.20260904", "v0.6.1", 1},
		{legacyRC9, legacyRC10, 1},
		{legacyRC9, dottedRC10, 1},
		{dottedRC9, legacyRC10, 1},
		{legacyRC10, dottedRC10, 0},
		{legacyRC10, legacyRC9, -1},
		{dottedRC10, legacyRC9, -1},
		{legacyRC10, dottedRC9, -1},
		{legacyRC10, "v0.16.0", 1},
		{"v0.16.0-rc99", "v0.16.0-rc100", 1},
		{"v0.16.0-rc09", dottedRC9, 0},
		{"v0.16.0-rc000", "v0.16.0-rc.0", 0},
		{"v0.16.0-rc99999999999999999999", "v0.16.0-rc100000000000000000000", 1},
		{legacyRC10 + "+build.1", dottedRC10 + "+build.2", 0},
		{"v0.16.0+build-rc9", "v0.16.0+build-rc10", 0},
		{"v0.16.0-alpha9", "v0.16.0-alpha10", -1},
		{"v0.16.0-rc9.extra", "v0.16.0-rc10.extra", -1},
	}
	for _, c := range cases {
		got := Compare(c.current, c.latest)
		if sign(got) != c.sign {
			t.Errorf("Compare(%q, %q) = %d, want sign %d", c.current, c.latest, got, c.sign)
		}
	}
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}
